package backup

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

const testPassword = "portable-backup-test-password"

func encryptedFixture(t *testing.T, data []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer, err := newEncryptedWriter(context.Background(), &output, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestAuthenticatedChunksRequireOrderingFinalFrameAndCompleteFile(t *testing.T) {
	plaintext := bytes.Repeat([]byte("large-backup-content"), 120000)
	encrypted := encryptedFixture(t, plaintext)
	reader, err := newEncryptedReader(context.Background(), bytes.NewReader(encrypted), testPassword)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(decoded, plaintext) || !reader.finished {
		t.Fatal("chunked encrypted stream did not round trip", err)
	}
	firstLength := 9 + chunkBytes + 16
	for _, fault := range []string{"password", "nonce", "tag", "sequence", "length", "flags", "missing-final", "truncate", "trailing", "reorder"} {
		t.Run(fault, func(t *testing.T) {
			data := append([]byte(nil), encrypted...)
			password := testPassword
			switch fault {
			case "password":
				password = "different-backup-password"
			case "nonce":
				data[24] ^= 1
			case "tag":
				data[len(data)-1] ^= 1
			case "sequence":
				data[headerBytes+3] = 1
			case "length":
				binary.BigEndian.PutUint32(data[headerBytes+4:headerBytes+8], ^uint32(0))
			case "flags":
				data[headerBytes+8] = 2
			case "missing-final":
				data = data[:len(data)-25]
			case "truncate":
				data = data[:len(data)/2]
			case "trailing":
				data = append(data, 'x')
			case "reorder":
				first := append([]byte(nil), data[headerBytes:headerBytes+firstLength]...)
				copy(data[headerBytes:headerBytes+firstLength], data[headerBytes+firstLength:headerBytes+firstLength*2])
				copy(data[headerBytes+firstLength:headerBytes+firstLength*2], first)
			}
			reader, err := newEncryptedReader(context.Background(), bytes.NewReader(data), password)
			if err == nil {
				_, err = io.Copy(io.Discard, reader)
			}
			if !errors.Is(err, ErrArchive) {
				t.Fatal("damaged stream was not rejected", err)
			}
		})
	}
}

func TestArchiveHeaderBoundsAndCancellation(t *testing.T) {
	valid := encryptedFixture(t, []byte("content"))
	for _, offset := range []int{0, 32, 36} {
		bad := append([]byte(nil), valid...)
		bad[offset] ^= 1
		if _, err := newEncryptedReader(context.Background(), bytes.NewReader(bad), testPassword); !errors.Is(err, ErrArchive) {
			t.Fatal("untrusted format or key-derivation parameters accepted", err)
		}
	}
	for _, password := range []string{"short", string([]byte{0xff}), string(bytes.Repeat([]byte("p"), 129))} {
		var output bytes.Buffer
		if _, err := newEncryptedWriter(context.Background(), &output, password); !errors.Is(err, ErrPassword) || output.Len() != 0 {
			t.Fatal("invalid password created archive data", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader, err := newEncryptedReader(ctx, bytes.NewReader(valid), testPassword)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := io.Copy(io.Discard, reader); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled verification continued", err)
	}
}
