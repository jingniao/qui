package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/release"
)

const localSocket = "agent-local.sock"
const localJobPrefix = "local-"

// LocalAgentStatus separates an authenticated installed image from a live worker.
// Only the supervisor can report Running. Stopped inspection never claims it.
type LocalAgentStatus struct {
	NodeID   string        `json:"node_id"`
	Revision int64         `json:"revision"`
	Current  updater.Image `json:"current"`
	Running  bool          `json:"running"`
	Ready    bool          `json:"ready"`
	Recovery bool          `json:"local_recovery"`
}

type localRequest struct {
	Operation string         `json:"operation"`
	Release   release.Signed `json:"release"`
}

type localResponse struct {
	Status LocalAgentStatus `json:"status"`
	Error  string           `json:"error,omitempty"`
}

type localCall struct {
	request localRequest
	answer  chan localResponse
	lease   <-chan struct{}
}

type localEndpoint struct {
	listener net.Listener
	requests chan localCall
	failures chan error
	cancel   context.CancelFunc
}

// The socket is private to the service account and root. The only operations are
// status, signed release preflight and signed release installation; no caller
// supplies a command, executable path, node identity or business revision.
func listenLocal(parent context.Context, directory string) (*localEndpoint, error) {
	path := filepath.Join(directory, localSocket)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("local Agent endpoint is not a socket: %s", path)
		}
		// Called only while holding the supervisor lifetime lock: any socket at
		// this point belongs to a previous, terminated supervisor.
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, errors.Join(err, listener.Close())
	}
	ctx, cancel := context.WithCancel(parent)
	e := &localEndpoint{listener: listener, requests: make(chan localCall), failures: make(chan error, 1), cancel: cancel}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if ctx.Err() == nil {
					e.failures <- err
				}
				return
			}
			go e.serve(ctx, conn)
		}
	}()
	return e, nil
}

func (e *localEndpoint) close() error {
	e.cancel()
	return e.listener.Close()
}

func (e *localEndpoint) serve(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	decoder := json.NewDecoder(io.LimitReader(conn, release.MaxManifestBytes+4096))
	decoder.DisallowUnknownFields()
	var request localRequest
	err := decoder.Decode(&request)
	var response localResponse
	var lease <-chan struct{}
	if err == nil {
		call := localCall{request: request, answer: make(chan localResponse, 1)}
		if request.Operation == "reserve" {
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				return
			}
			done := make(chan struct{})
			call.lease = done
			lease = done
			go func() {
				_, err := io.Copy(io.Discard, conn)
				if err != nil && ctx.Err() == nil {
					slog.Error("local Agent bootstrap lease disconnected", "error", err)
				}
				close(done)
			}()
		}
		select {
		case e.requests <- call:
		case <-ctx.Done():
			return
		}
		select {
		case response = <-call.answer:
		case <-ctx.Done():
			return
		}
	} else {
		response.Error = err.Error()
	}
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return
	}
	// A disconnected terminal cannot cancel a journalled installation. The
	// worker and the durable result continue independently of this connection.
	if err := json.NewEncoder(conn).Encode(response); err != nil {
		slog.Error("local Agent result delivery failed; installation result remains in the journal", "error", err)
		return
	}
	if request.Operation == "reserve" && response.Error == "" {
		select {
		case <-ctx.Done():
		case <-lease:
		}
	}
}

func callLocal(ctx context.Context, directory string, request localRequest) (LocalAgentStatus, error) {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", filepath.Join(directory, localSocket))
	if err != nil {
		return LocalAgentStatus{}, fmt.Errorf("本地 Agent 升级入口不可用；旧安装请先更新引导程序并重跑同版本命令: %w", err)
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopCancel()
	deadline := time.Now().Add(5 * time.Minute)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return LocalAgentStatus{}, err
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return LocalAgentStatus{}, err
	}
	var response localResponse
	decoder := json.NewDecoder(io.LimitReader(conn, release.MaxManifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response.Status, fmt.Errorf("本地升级结果未确认；保留安装记录，检查服务状态后重跑命令: %w", err)
	}
	if response.Error != "" {
		return response.Status, errors.New(response.Error)
	}
	return response.Status, nil
}

// LocalStatus uses explicit stopped inspection rather than guessing a runtime
// version from an executable on disk when the service cannot be contacted.
func LocalStatus(configPath string, stopped bool) (status LocalAgentStatus, resultErr error) {
	c, err := configuration.LoadAgent(configPath)
	if err != nil {
		return status, err
	}
	active, err := Active(c.DataDirectory)
	if err != nil || !active {
		return status, err
	}
	if !stopped {
		return callLocal(context.Background(), c.DataDirectory, localRequest{Operation: "status"})
	}
	owner, err := lifecycle.TrySupervisor(c.DataDirectory)
	if err != nil {
		return status, err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	worker, err := lifecycle.Try(owner.Directory())
	if err != nil {
		return status, err
	}
	defer func() { resultErr = errors.Join(resultErr, worker.Close()) }()
	node, err := identity.Load(filepath.Join(owner.Directory(), "identity"))
	if err != nil {
		return status, err
	}
	key, err := readOriginal(filepath.Join(owner.Directory(), "configuration.key"), 32)
	if err != nil {
		return status, err
	}
	defer clear(key)
	if _, err := readRecord(filepath.Join(owner.Directory(), "identity", markerName), key, node.NodeID); err != nil {
		return status, err
	}
	state, err := updater.InspectAgentInstallation(updater.HistoryOptions{Directory: filepath.Join(owner.Directory(), "agent-release-history"), Checkpoint: filepath.Join(owner.Directory(), "identity", "agent-release-checkpoint.enc"), NodeID: node.NodeID, Key: key})
	if err != nil {
		return status, err
	}
	return LocalAgentStatus{NodeID: node.NodeID, Current: state.Current}, nil
}
