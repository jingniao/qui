package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"proxycontrol/internal/platform/pki"
)

const enrollmentAttemptName = "enrollment-attempt.json"

// hostNetworkDirectoryName is the helper directory install.sh creates in the
// data directory (control.go joins the same name).
const hostNetworkDirectoryName = "host-network"

type enrollmentAttempt struct {
	Version       int    `json:"version"`
	TokenSHA256   string `json:"tokenSha256"`
	ControlSHA256 string `json:"controlSha256"`
}

func enrollmentDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

// This is local retry context, not signed evidence or recovery authorization.
// The stopped-node locks are owned by the caller. It retains the attempt after
// enrollment; a missing identity does not permit a token switch over old state.
func PrepareEnrollmentAttempt(ctx context.Context, directory, tokenDigest, controlDigest string) error {
	if !enrollmentDigest(tokenDigest) || !enrollmentDigest(controlDigest) {
		return errors.New("登记尝试绑定无效")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	identityDir := filepath.Join(directory, "identity")
	info, err := os.Lstat(identityDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("登记身份目录不安全，保留原材料")
	}
	path := filepath.Join(directory, enrollmentAttemptName)
	data, readErr := readMaterialFile(path, 4096)
	present := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return errors.New("原登记尝试无法读取，保留材料")
	}
	if present {
		var original enrollmentAttempt
		if pki.DecodeTransitionJSON(data, &original) != nil || original.Version != 1 || original.TokenSHA256 != tokenDigest || original.ControlSHA256 != controlDigest {
			return errors.New("已有登记尝试与本次Token或控制端绑定不同，不能覆盖或复用原私钥")
		}
	}
	if err := requireEnrollmentDirectory(ctx, directory, present); err != nil {
		return err
	}
	if present {
		return nil
	}
	// Publish the attempt before creating a private key. Exclusive creation does
	// not truncate a raced-in attempt; an interrupted partial marker is retained.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write([]byte(`{"version":1,"tokenSha256":"` + tokenDigest + `","controlSha256":"` + controlDigest + `"}`)); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err = file.Close(); err != nil {
		return err
	}
	return syncAckDirectory(directory)
}
func requireEnrollmentDirectory(ctx context.Context, directory string, attemptPresent bool) error {
	return requireEnrollmentDirectoryMode(ctx, directory, attemptPresent, false, "")
}
func requireEnrollmentResultDirectory(ctx context.Context, directory string) error {
	return requireEnrollmentDirectoryMode(ctx, directory, true, true, "")
}
func requireEnrollmentDirectoryMode(ctx context.Context, directory string, attemptPresent, resultPresent bool, ownTemporary string) error {
	for _, folder := range []string{directory, filepath.Join(directory, "identity")} {
		file, err := os.Open(folder)
		if err != nil {
			return err
		}
		entries, readErr := file.ReadDir(65)
		closeErr := file.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) || closeErr != nil || len(entries) > 64 {
			return errors.New("登记目录超出检查预算或不可读，保留原状态")
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := entry.Name()
			root := folder == directory
			// install.sh creates host-network/ for the enforced-source helper
			// before the first enrollment.
			allowed := name == ".agent-owner.lock" || name == ".agent-supervisor.lock" || root && (name == "identity" || name == hostNetworkDirectoryName) || root && attemptPresent && name == enrollmentAttemptName || !root && attemptPresent && name == "client.key"
			allowed = allowed || resultPresent && (root && name == enrollmentResultName || !root && (name == "client.crt" || name == "node-ca.crt" || name == "identity.json"))
			allowed = allowed || root && ownTemporary != "" && name == ownTemporary
			if !allowed {
				return errors.New("登记目录保留身份、配置、账本或未知材料；不能因身份元数据缺失重新登记，请保留旧目录并使用独立新目录")
			}
			info, err := os.Lstat(filepath.Join(folder, name))
			if err != nil {
				return err
			}
			if root && name == "identity" {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return errors.New("登记身份目录类型异常")
				}
				continue
			}
			if root && name == hostNetworkDirectoryName {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return errors.New("登记目录中的 host-network 类型异常")
				}
				continue
			}
			if !info.Mode().IsRegular() {
				return errors.New("登记材料类型异常，保留原目录")
			}
			if name == "client.key" {
				key, err := readMaterialFile(filepath.Join(folder, name), 32768)
				clear(key)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Bind both endpoint and the exact configured public server CA bytes. Never
// store the token or CA contents in the local retry marker.
func EnrollmentControlDigest(address, serverName, caPath string) (string, error) {
	file, err := os.Open(caPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > 1<<20 {
		return "", errors.New("登记控制端CA材料预算无效")
	}
	hash := sha256.New()
	hash.Write([]byte(address))
	hash.Write([]byte{0})
	hash.Write([]byte(serverName))
	hash.Write([]byte{0})
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil)), nil
}
