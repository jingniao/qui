// proxy-release is an offline operator tool. Signing private keys are never
// sent to a panel or Agent through the control protocol.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/agenthost"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
	"proxycontrol/pkg/contracts/release"
	"runtime"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("use keygen, sign, verify, stage, fetch, prepare-agent, queue-agent or agent-result")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	privatePath := flags.String("private-key", "", "offline private key file")
	publicPath := flags.String("public-key", "", "pinned public key file")
	manifestPath := flags.String("manifest", "", "signed manifest file")
	artifactPath := flags.String("artifact", "", "raw executable file")
	outputPath := flags.String("out", "", "output manifest or candidate directory")
	component := flags.String("component", "", "proxy-agent, xray or sing-box")
	version := flags.String("version", "", "release version")
	platformOS := flags.String("os", runtime.GOOS, "release OS")
	architecture := flags.String("arch", runtime.GOARCH, "release architecture")
	sequence := flags.Int64("sequence", 0, "publisher monotonic release sequence")
	minimum := flags.Int64("minimum-sequence", 0, "minimum permitted sequence")
	validFor := flags.Duration("valid-for", 30*24*time.Hour, "signature validity")
	agentConfig := flags.String("agent-config", "", "local enrolled Agent configuration for fetch")
	initializeHistory := flags.Bool("initialize-history", false, "explicit first-time release history creation")
	requestID := flags.String("request-id", "", "immutable local Agent update request ID")
	revision := flags.Int64("revision", 0, "confirmed Agent configuration revision")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return errors.New("invalid release arguments")
	}
	if args[0] == "queue-agent" || args[0] == "agent-result" {
		allowed := true
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "agent-config" && f.Name != "request-id" && (args[0] != "queue-agent" || f.Name != "manifest" && f.Name != "revision") {
				allowed = false
			}
		})
		if !allowed || *agentConfig == "" || !release.ValidUpdateID(*requestID) {
			return errors.New("Agent task requires only its configuration, request ID and, for queueing, manifest and revision")
		}
		if args[0] == "agent-result" {
			job, err := agenthost.UpdateStatus(*agentConfig, *requestID)
			if err != nil {
				return err
			}
			status := "queued"
			if job.Result != nil {
				status = job.Result.Status
			}
			return json.NewEncoder(output).Encode(struct {
				ID     string                `json:"id"`
				Status string                `json:"status"`
				Result *release.UpdateResult `json:"result,omitempty"`
			}{*requestID, status, job.Result})
		}
		if *manifestPath == "" || *revision < 1 {
			return errors.New("queue-agent requires --manifest and a positive --revision")
		}
		file, err := os.Open(*manifestPath)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(file, release.MaxManifestBytes+1))
		file.Close()
		if err != nil {
			return err
		}
		signed, err := release.Decode(data)
		if err != nil {
			return err
		}
		request, err := agenthost.QueueUpdate(*agentConfig, *requestID, *revision, signed)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "queued-agent", request.ID, request.Release.Manifest.Version)
		return err
	}
	if args[0] == "prepare-agent" {
		allowed := true
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "agent-config" && f.Name != "manifest" && f.Name != "artifact" {
				allowed = false
			}
		})
		if !allowed || *agentConfig == "" || *manifestPath == "" || *artifactPath == "" {
			return errors.New("prepare-agent requires only --agent-config, --manifest and --artifact")
		}
		result, err := prepareAgent(context.Background(), *agentConfig, *manifestPath, *artifactPath)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "agent-prepared", result.Contract.AgentVersion, result.Image.SHA256, "storage-compatible")
		return err
	}
	if args[0] == "fetch" {
		if *agentConfig == "" || *sequence < 1 || !release.Component(*component) {
			return errors.New("fetch requires --agent-config, --component and --sequence")
		}
		candidate, err := fetchRelease(context.Background(), *agentConfig, *component, *sequence, *initializeHistory)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "staged", candidate.Manifest.Component, candidate.Manifest.Version, candidate.SHA256)
		return err
	}
	if args[0] == "keygen" {
		if *privatePath == "" || *publicPath == "" || *privatePath == *publicPath {
			return errors.New("distinct private and public key files required")
		}
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		defer clear(private)
		if err := writeKeyPair(*privatePath, *publicPath, private, public); err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "release key:", release.KeyID(public))
		return err
	}
	if args[0] == "sign" {
		private, err := keyFile(*privatePath, ed25519.PrivateKeySize)
		if err != nil {
			return err
		}
		defer clear(private)
		file, err := os.Open(*artifactPath)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > release.MaxArtifactBytes {
			return updater.ErrArtifact
		}
		hash := sha256.New()
		size, err := io.Copy(hash, io.LimitReader(file, release.MaxArtifactBytes+1))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		manifest := release.Manifest{SchemaVersion: 1, Component: *component, Version: *version, OS: *platformOS, Architecture: *architecture, Sequence: *sequence, Bytes: size, SHA256: hex.EncodeToString(hash.Sum(nil)), IssuedAt: now, ExpiresAt: now.Add(*validFor)}
		signed, err := release.Sign(manifest, ed25519.PrivateKey(private))
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(signed, "", "  ")
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if err := exclusive(*outputPath, data, 0644); err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "signed", manifest.Component, manifest.Version, manifest.SHA256)
		return err
	}
	if args[0] != "verify" && args[0] != "stage" {
		return errors.New("unknown release operation")
	}
	public, err := keyFile(*publicPath, ed25519.PublicKeySize)
	if err != nil {
		return err
	}
	file, err := os.Open(*manifestPath)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, release.MaxManifestBytes+1))
	file.Close()
	if err != nil {
		return err
	}
	signed, err := release.Decode(data)
	if err != nil {
		return err
	}
	policy := release.Policy{Component: *component, OS: *platformOS, Architecture: *architecture, MinimumSequence: *minimum, Now: time.Now().UTC(), Keys: map[string]release.TrustedKey{release.KeyID(public): {PublicKey: public, Components: []string{*component}}}}
	if err := signed.Verify(policy); err != nil {
		return err
	}
	if args[0] == "verify" {
		_, err = fmt.Fprintln(output, "verified", signed.Manifest.Component, signed.Manifest.Version, signed.Manifest.SHA256)
		return err
	}
	artifact, err := os.Open(*artifactPath)
	if err != nil {
		return err
	}
	defer artifact.Close()
	if *outputPath == "" {
		return errors.New("candidate directory required")
	}
	candidate, err := updater.Stage(context.Background(), *outputPath, signed, policy, artifact)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "staged", candidate.Manifest.Component, candidate.Manifest.Version, candidate.SHA256)
	return err
}

func fetchRelease(ctx context.Context, path, component string, sequence int64, initialize bool) (updater.Candidate, error) {
	c, err := config.LoadAgent(path)
	if err != nil {
		return updater.Candidate{}, err
	}
	if !c.Updates.Enabled() {
		return updater.Candidate{}, errors.New("local Agent release trust is not configured")
	}
	state, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		return updater.Candidate{}, err
	}
	// Load the existing configuration identity key without creating or
	// replacing it. An update cannot repair a lost accounting/identity store.
	key, err := os.ReadFile(filepath.Join(c.DataDirectory, "configuration.key"))
	if err != nil {
		return updater.Candidate{}, err
	}
	defer clear(key)
	options := updater.HistoryOptions{Directory: filepath.Join(c.DataDirectory, "release-history"), Checkpoint: filepath.Join(state.Directory, "release-checkpoint.enc"), NodeID: state.NodeID, Key: key}
	var history *updater.History
	if initialize {
		history, err = updater.CreateHistory(options)
	} else {
		history, err = updater.OpenHistory(options)
	}
	if err != nil {
		return updater.Candidate{}, err
	}
	defer history.Close()
	keys, err := release.PinnedKeys(c.Updates.Keys)
	if err != nil {
		return updater.Candidate{}, err
	}
	trust, err := c.Updates.TrustConfig()
	if err != nil {
		return updater.Candidate{}, err
	}
	downloader, err := updater.NewDownloader(c.Updates.Catalogue, trust)
	if err != nil {
		return updater.Candidate{}, err
	}
	defer downloader.Close()
	policy := release.Policy{Component: component, OS: runtime.GOOS, Architecture: runtime.GOARCH, Now: time.Now().UTC(), Keys: keys}
	return downloader.Fetch(ctx, filepath.Join(c.DataDirectory, "releases", "candidates"), sequence, policy, history)
}

// Reserve both output paths before writing either key. On failure, remove only
// files still identical to those opened here, never a pre-existing public key.
func writeKeyPair(privatePath, publicPath string, private, public []byte) (resultErr error) {
	privateAbsolute, err := filepath.Abs(privatePath)
	if err != nil {
		return err
	}
	publicAbsolute, err := filepath.Abs(publicPath)
	if err != nil {
		return err
	}
	if privateAbsolute == publicAbsolute || runtime.GOOS == "windows" && strings.EqualFold(privateAbsolute, publicAbsolute) {
		return errors.New("distinct private and public key files required")
	}
	privateFile, err := os.OpenFile(privatePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	privateInfo, err := privateFile.Stat()
	if err != nil {
		privateFile.Close()
		return err
	}
	var publicFile *os.File
	var publicInfo os.FileInfo
	defer func() {
		privateClose := privateFile.Close()
		var publicClose error
		if publicFile != nil {
			publicClose = publicFile.Close()
		}
		resultErr = errors.Join(resultErr, privateClose, publicClose)
		if resultErr != nil {
			resultErr = errors.Join(resultErr, removeOwned(privatePath, privateInfo))
			if publicInfo != nil {
				resultErr = errors.Join(resultErr, removeOwned(publicPath, publicInfo))
			}
		}
	}()
	publicFile, err = os.OpenFile(publicPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	publicInfo, err = publicFile.Stat()
	if err != nil {
		return err
	}
	for _, item := range []struct {
		file *os.File
		data []byte
	}{
		{privateFile, []byte(base64.StdEncoding.EncodeToString(private) + "\n")},
		{publicFile, []byte(base64.StdEncoding.EncodeToString(public) + "\n")},
	} {
		if _, err := item.file.Write(item.data); err != nil {
			return err
		}
		if err := item.file.Sync(); err != nil {
			return err
		}
	}
	return nil
}
func removeOwned(path string, owned os.FileInfo) error {
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(current, owned) {
		return errors.New("release output ownership changed; retained for investigation")
	}
	return os.Remove(path)
}

func keyFile(path string, size int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return nil, release.ErrSignature
	}
	encoded := strings.TrimSpace(string(data))
	clear(data)
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != size || base64.StdEncoding.EncodeToString(key) != encoded {
		clear(key)
		return nil, release.ErrSignature
	}
	return key, nil
}
func exclusive(path string, data []byte, mode os.FileMode) error {
	if path == "" {
		return errors.New("output path required")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
