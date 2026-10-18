package backup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrReceive = errors.New("invalid or oversized portable backup upload")

// Receive writes a bounded encrypted upload into protected staging. The
// callback runs only after complete archive/database/material verification.
// It must publish under the caller's directory process lock.
func Receive(ctx context.Context, directory string, source io.Reader, password string, publish func(Result, string) error) (result Result, resultErr error) {
	if err := ValidatePassword(password); err != nil {
		return result, err
	}
	if source == nil || publish == nil {
		return result, ErrReceive
	}
	work, err := newWork(directory)
	if err != nil {
		return result, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, removeWork(directory, work))
		if resultErr != nil {
			resultErr = errors.Join(resultErr, ctx.Err())
		}
	}()
	path := filepath.Join(work, "received.pcb")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	n, copyErr := io.Copy(file, io.LimitReader(contextReader{ctx, source}, MaxArchiveBytes+1))
	if copyErr == nil && (n < headerBytes || n > MaxArchiveBytes) {
		copyErr = ErrReceive
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return result, err
	}
	result, err = Inspect(ctx, directory, path, password)
	if err != nil {
		return result, err
	}
	if err := publish(result, path); err != nil {
		return result, err
	}
	return result, nil
}
