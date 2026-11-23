//go:build linux

package xray

import (
	"io"
	"path/filepath"
	"proxycontrol/internal/agent/core"
)

const accessLogName = "access.log"
const accessLogLimit = 20 << 20

type accessLogTail struct{ *core.FileTail }

func (a *Adapter) accessLogPath() string { return filepath.Join(a.Directory, accessLogName) }

func newAccessLogTail(path string, sink io.Writer, limit int64) (*accessLogTail, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	t, err := core.OpenFileTail(absolute, sink, limit, nil)
	if err != nil {
		return nil, err
	}
	return &accessLogTail{t}, nil
}

func (t *accessLogTail) close() error { return t.CloseAfterExit() }

func (a *Adapter) closeAccessLogLocked() error {
	if a.accessTail == nil {
		return nil
	}
	err := a.accessTail.close()
	a.accessTail = nil
	return err
}
