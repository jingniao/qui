package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"time"

	"proxycontrol/pkg/contracts/release"
)

// ReserveBootstrap keeps the jobs locks in the existing supervisor until the
// shell closes stdin or stops that supervisor. A killed shell closes its coproc
// pipe, so a lost terminal cannot strand a persistent reservation.
func ReserveBootstrap(ctx context.Context, configPath, manifestPath, binaryPath, version string, input io.Reader, output io.Writer) error {
	c, signed, err := prepareLocalInput(ctx, configPath, manifestPath, binaryPath, version)
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", filepath.Join(c.DataDirectory, localSocket))
	if err != nil {
		return err
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopCancel()
	if err := conn.SetDeadline(time.Now().Add(time.Minute)); err != nil {
		return err
	}
	if err := json.NewEncoder(conn).Encode(localRequest{Operation: "reserve", Release: signed}); err != nil {
		return err
	}
	var response localResponse
	decoder := json.NewDecoder(io.LimitReader(conn, release.MaxManifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(output, "proxy-agent-bootstrap-v1 reserved"); err != nil {
		return err
	}
	parentDone := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, input); parentDone <- err }()
	supervisorDone := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := conn.Read(b[:])
		if errors.Is(err, io.EOF) {
			err = nil // Expected when the reserved supervisor is stopped.
		} else if err == nil {
			err = errors.New("unexpected data on the bootstrap reservation connection")
		}
		supervisorDone <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-parentDone:
		return err
	case err := <-supervisorDone:
		return err
	}
}
