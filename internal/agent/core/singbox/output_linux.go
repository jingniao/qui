//go:build linux

package singbox

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"syscall"
)

func (a *Adapter) openOutputLocked(command *exec.Cmd, config core.RenderedConfig) error {
	path, err := filepath.Abs(filepath.Join(a.Directory, "output.log"))
	if err != nil {
		return err
	}
	var sink io.Writer = io.Discard
	a.sourceWriter = nil
	if config.AccessContext != nil && a.hooks.AccessSink != nil {
		a.sourceWriter = collector.NewSingboxWriter(config.AccessContext, a.hooks.AccessSink, a.hooks.AccessRejected)
		sink = a.sourceWriter
	}
	tail, err := core.OpenFileTail(path, sink, outputLogLimit, nil)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return errors.Join(err, tail.CloseAfterExit())
	}
	info, statErr := file.Stat()
	cp, checkpointErr := tail.Checkpoint()
	tail.Resume()
	if err := errors.Join(statErr, checkpointErr); err != nil {
		return errors.Join(err, file.Close(), tail.CloseAfterExit())
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Nlink != 1 || uint64(stat.Dev) != cp.Device || stat.Ino != cp.Inode {
		return errors.Join(errors.New("sing-box output descriptor differs from file tail"), file.Close(), tail.CloseAfterExit())
	}
	command.Stdout = file
	a.outputTail = tail
	return nil
}

func closeCommandOutput(command *exec.Cmd) error {
	if file, ok := command.Stdout.(*os.File); ok {
		return file.Close()
	}
	return nil
}
