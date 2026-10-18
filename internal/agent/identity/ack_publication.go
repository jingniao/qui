package identity

import (
	"errors"
	"path/filepath"
)

var ErrAckPublishedUnconfirmed = errors.New("acknowledgment file published but directory persistence unconfirmed")

func publishAck(path string, data []byte) error {
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	if err := syncAckDirectory(filepath.Dir(path)); err != nil {
		return errors.Join(ErrAckPublishedUnconfirmed, err)
	}
	return nil
}
