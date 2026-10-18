// Package vault encrypts persisted credentials and configuration snapshots.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

type Vault struct{ aead cipher.AEAD }

func Open(path string) (*Vault, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		if _, err := file.Write(key); err != nil {
			file.Close()
			return nil, err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
		if err := SyncDir(filepath.Dir(path)); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return FromKey(key)
}

// FromKey loads already supplied key material without generating a replacement.
// Backup validation uses it to verify the original encrypted database records.
func FromKey(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("invalid encryption key; restore original key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}
func (v *Vault) Seal(data []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	result := append([]byte{1}, nonce...)
	return v.aead.Seal(result, nonce, data, []byte("proxy-control-config-v1")), nil
}
func (v *Vault) Open(data []byte) ([]byte, error) {
	size := v.aead.NonceSize()
	if len(data) < 1+size+v.aead.Overhead() || data[0] != 1 {
		return nil, errors.New("invalid encrypted configuration")
	}
	plaintext, err := v.aead.Open(nil, data[1:1+size], data[1+size:], []byte("proxy-control-config-v1"))
	if err != nil {
		return nil, errors.New("configuration integrity verification failed")
	}
	return plaintext, nil
}
