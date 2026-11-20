// Package systemupdate transports fixed requests and root-owned observations.
// Installation remains entirely in the installed terminal updater.
package systemupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

const published = "/var/lib/proxy-agent-update"

type Client struct{ directory, nodeID string }

func New(directory, nodeID string) *Client {
	return &Client{filepath.Join(directory, "system-update"), nodeID}
}
func atomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".request-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func read(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("system update observation is not a regular file: %s", path)
	}
	return os.ReadFile(path)
}
func (c *Client) submit(r release.AgentSystemUpdateRequest) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.NodeID != c.nodeID {
		return fmt.Errorf("system update node identity mismatch")
	}
	if err := os.MkdirAll(c.directory, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	path := filepath.Join(c.directory, "request.json")
	previous, err := read(path)
	if err == nil {
		old, decodeErr := release.DecodeAgentSystemUpdateRequest(previous)
		if decodeErr != nil {
			return decodeErr
		}
		if old.Operation == "probe" {
			return atomic(path, data)
		}
		if _, readErr := read(filepath.Join(published, old.AttemptID, "result.json")); readErr == nil {
			return atomic(path, data)
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		if !bytes.Equal(previous, data) {
			return fmt.Errorf("another system update request is awaiting root acceptance")
		}
		return atomic(path, data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomic(path, data)
}
func (c *Client) Probe(ctx context.Context, revision int64) release.AgentSystemUpdateProbe {
	p := release.AgentSystemUpdateProbe{NodeID: c.nodeID}
	if _, err := os.Stat(published); err != nil {
		p.Reason = err.Error()
		return p
	}
	started := time.Now().UTC()
	err := c.submit(release.AgentSystemUpdateRequest{SchemaVersion: 1, Operation: "probe", TaskID: "probe", AttemptID: "probe", NodeID: c.nodeID, Revision: revision})
	if err != nil {
		p.Reason = err.Error()
		return p
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		data, err := read(filepath.Join(published, "probe.json"))
		if err == nil {
			var observed release.AgentSystemUpdateProbe
			if err = json.Unmarshal(data, &observed); err == nil {
				at, parseErr := time.Parse(time.RFC3339Nano, observed.ObservedAt)
				if parseErr == nil && at.After(started) && observed.SchemaVersion == 1 && observed.NodeID == c.nodeID && observed.AttemptID == "probe" {
					observed.Supported = observed.Supported && observed.UpdaterAvailable && observed.WatcherReady
					return observed
				}
			}
		}
		select {
		case <-ctx.Done():
			p.Reason = ctx.Err().Error()
			return p
		case <-deadline.C:
			if err != nil {
				p.Reason = err.Error()
			} else {
				p.Reason = "root system update probe did not acknowledge this startup"
			}
			return p
		case <-tick.C:
		}
	}
}
func (c *Client) Submit(command *pb.AgentSystemUpdateCommand, current int64) error {
	if command == nil {
		return fmt.Errorf("missing system update request")
	}
	r, err := release.DecodeAgentSystemUpdateRequest(command.RequestJson)
	if err != nil {
		return err
	}
	if r.Operation != "upgrade" || r.Revision != current {
		return fmt.Errorf("system update business revision differs from the running revision")
	}
	if err := os.MkdirAll(c.directory, 0700); err != nil {
		return err
	}
	path := filepath.Join(c.directory, r.AttemptID+".request.json")
	previous, err := read(path)
	if err == nil {
		if !bytes.Equal(previous, command.RequestJson) {
			return fmt.Errorf("system update attempt was reused with different content")
		}
		return c.submit(r)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := atomic(path, command.RequestJson); err != nil {
		return err
	}
	return c.submit(r)
}

// SubmissionError preserves a delivery failure independently of the root
// conclusion. A later accepted request may still complete successfully.
func (c *Client) SubmissionError(command *pb.AgentSystemUpdateCommand, cause error) error {
	r, err := release.DecodeAgentSystemUpdateRequest(command.RequestJson)
	if err != nil {
		return err
	}
	if r.NodeID != c.nodeID {
		return fmt.Errorf("submission error node mismatch")
	}
	if err = os.MkdirAll(c.directory, 0700); err != nil {
		return err
	}
	requestPath := filepath.Join(c.directory, r.AttemptID+".request.json")
	if old, readErr := read(requestPath); errors.Is(readErr, os.ErrNotExist) {
		if err = atomic(requestPath, command.RequestJson); err != nil {
			return err
		}
	} else if readErr != nil {
		return readErr
	} else if !bytes.Equal(old, command.RequestJson) {
		return fmt.Errorf("submission error attempt mismatch")
	}
	path := filepath.Join(c.directory, r.AttemptID+".error.json")
	if _, err = read(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	p := release.AgentSystemUpdateProgress{AttemptID: r.AttemptID, Phase: "request_submit", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Error: cause.Error()}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return atomic(path, data)
}
func (c *Client) ack(attempt string) (release.AgentSystemUpdateAck, error) {
	a := release.AgentSystemUpdateAck{AttemptID: attempt}
	data, err := read(filepath.Join(c.directory, attempt+".ack.json"))
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	err = json.Unmarshal(data, &a)
	return a, err
}
func (c *Client) Acknowledge(data []byte) error {
	var a release.AgentSystemUpdateAck
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	request, err := read(filepath.Join(c.directory, a.AttemptID+".request.json"))
	if err != nil {
		return err
	}
	r, err := release.DecodeAgentSystemUpdateRequest(request)
	if err != nil || r.AttemptID != a.AttemptID {
		return fmt.Errorf("system update acknowledgment has no matching request")
	}
	old, err := c.ack(a.AttemptID)
	if err != nil {
		return err
	}
	if a.LogOffset < old.LogOffset || a.LogSequence < old.LogSequence || a.ProgressSequence < old.ProgressSequence || old.ResultSHA256 != "" && a.ResultSHA256 != old.ResultSHA256 {
		return fmt.Errorf("system update acknowledgment regressed")
	}
	return atomic(filepath.Join(c.directory, a.AttemptID+".ack.json"), data)
}

// Next never removes root output. Durable ACKs allow reconnects to resume at the
// last committed byte, and root retains the complete output for operator review.
func (c *Client) Next() (*pb.AgentSystemUpdateEvent, error) {
	entries, err := os.ReadDir(c.directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" || !bytes.HasSuffix([]byte(entry.Name()), []byte(".request.json")) {
			continue
		}
		data, err := read(filepath.Join(c.directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		r, err := release.DecodeAgentSystemUpdateRequest(data)
		if err != nil {
			return nil, err
		}
		a, err := c.ack(r.AttemptID)
		if err != nil {
			return nil, err
		}
		root := filepath.Join(published, r.AttemptID)
		interrupted := false
		if data, err := read(filepath.Join(c.directory, r.AttemptID+".error.json")); err == nil {
			digest := sha256.Sum256(data)
			if hex.EncodeToString(digest[:]) != a.DispatchErrorSHA256 {
				return &pb.AgentSystemUpdateEvent{ProgressJson: data}, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		progress, err := read(filepath.Join(root, "progress.json"))
		if err == nil {
			var p release.AgentSystemUpdateProgress
			if err = json.Unmarshal(progress, &p); err != nil {
				return nil, err
			}
			if p.AttemptID != r.AttemptID {
				return nil, fmt.Errorf("root progress identity mismatch")
			}
			interrupted = p.Phase == "interrupted"
			if p.Sequence > a.ProgressSequence {
				return &pb.AgentSystemUpdateEvent{ProgressJson: progress}, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		f, err := os.Open(filepath.Join(root, "output.log"))
		if err == nil {
			if _, err = f.Seek(a.LogOffset, io.SeekStart); err != nil {
				f.Close()
				return nil, err
			}
			content := make([]byte, 65536)
			n, readErr := f.Read(content)
			closeErr := f.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if n > 0 {
				content = content[:n]
				digest := sha256.Sum256(content)
				chunk := release.AgentSystemUpdateLogChunk{AttemptID: r.AttemptID, Sequence: a.LogSequence + 1, Offset: a.LogOffset, SHA256: hex.EncodeToString(digest[:]), Content: content}
				encoded, err := json.Marshal(chunk)
				return &pb.AgentSystemUpdateEvent{LogChunkJson: encoded}, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		result, err := read(filepath.Join(root, "result.json"))
		if err == nil {
			v, err := release.DecodeAgentSystemUpdateResult(result)
			if err != nil {
				return nil, err
			}
			if v.AttemptID != r.AttemptID || v.NodeID != r.NodeID || v.TaskID != r.TaskID || v.Revision != r.Revision || v.TargetVersion != r.TargetVersion {
				return nil, fmt.Errorf("root system update result identity mismatch")
			}
			digest := sha256.Sum256(result)
			if hex.EncodeToString(digest[:]) != a.ResultSHA256 {
				return &pb.AgentSystemUpdateEvent{ResultJson: result}, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		// A restart between local retention and root acceptance resubmits the
		// original bytes. Root's private acceptance record prevents re-execution.
		if a.ResultSHA256 == "" {
			if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) || interrupted {
				if err := c.submit(r); err != nil {
					return nil, err
				}
			}
		}
	}
	return nil, nil
}
