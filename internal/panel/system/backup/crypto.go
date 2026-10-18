// Package backup implements bounded, authenticated portable panel archives.
package backup

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	chunkBytes       = 1 << 20
	keyIterations    = 600000
	headerBytes      = 40
	MaxUnpackedBytes = int64(1 << 30)
	MaxArchiveBytes  = MaxUnpackedBytes + (16 << 20)
)

var ErrArchive = errors.New("backup authentication or integrity verification failed")
var ErrPassword = errors.New("backup password must contain 12 to 128 valid characters")

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || utf8.RuneCountInString(password) > 128 {
		return ErrPassword
	}
	return nil
}

func archiveCipher(header []byte, password string) (cipher.AEAD, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	key, err := pbkdf2.Key(sha256.New, password, header[8:24], keyIterations, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

type encryptedWriter struct {
	ctx         context.Context
	destination io.Writer
	header      []byte
	aead        cipher.AEAD
	sequence    uint32
	buffer      []byte
	closed      bool
	err         error
}

func newEncryptedWriter(ctx context.Context, destination io.Writer, password string) (*encryptedWriter, error) {
	header := make([]byte, headerBytes)
	copy(header, "PCBKP001")
	if _, err := rand.Read(header[8:32]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint32(header[32:36], chunkBytes)
	binary.BigEndian.PutUint32(header[36:40], keyIterations)
	aead, err := archiveCipher(header, password)
	if err != nil {
		return nil, err
	}
	if err := writeAll(destination, header); err != nil {
		return nil, err
	}
	return &encryptedWriter{ctx: ctx, destination: destination, header: header, aead: aead, buffer: make([]byte, 0, chunkBytes)}, nil
}

func writeAll(destination io.Writer, data []byte) error {
	n, err := destination.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func (w *encryptedWriter) frame(data []byte, final bool) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if w.sequence >= uint32(MaxArchiveBytes/chunkBytes)+2 {
		return ErrArchive
	}
	frame := make([]byte, 9)
	binary.BigEndian.PutUint32(frame[:4], w.sequence)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(data)+w.aead.Overhead()))
	if final {
		frame[8] = 1
	}
	nonce := make([]byte, 12)
	copy(nonce, w.header[24:32])
	binary.BigEndian.PutUint32(nonce[8:], w.sequence)
	aad := append(append([]byte(nil), w.header...), frame...)
	sealed := w.aead.Seal(nil, nonce, data, aad)
	if err := writeAll(w.destination, frame); err != nil {
		return err
	}
	if err := writeAll(w.destination, sealed); err != nil {
		return err
	}
	w.sequence++
	return nil
}

func (w *encryptedWriter) Write(data []byte) (int, error) {
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	if w.err != nil {
		return 0, w.err
	}
	total := 0
	for len(data) > 0 {
		n := min(len(data), chunkBytes-len(w.buffer))
		w.buffer = append(w.buffer, data[:n]...)
		data = data[n:]
		total += n
		if len(w.buffer) == chunkBytes {
			if err := w.frame(w.buffer, false); err != nil {
				w.err = err
				return total, err
			}
			w.buffer = w.buffer[:0]
		}
	}
	return total, nil
}

func (w *encryptedWriter) Close() error {
	if w.closed {
		return w.err
	}
	w.closed = true
	defer clear(w.buffer[:cap(w.buffer)])
	if w.err != nil {
		return w.err
	}
	if len(w.buffer) > 0 {
		w.err = w.frame(w.buffer, false)
	}
	if w.err == nil {
		w.err = w.frame(nil, true)
	}
	return w.err
}

type encryptedReader struct {
	ctx      context.Context
	source   io.Reader
	header   []byte
	aead     cipher.AEAD
	sequence uint32
	buffer   []byte
	finished bool
	bytes    int64
}

func newEncryptedReader(ctx context.Context, source io.Reader, password string) (*encryptedReader, error) {
	header := make([]byte, headerBytes)
	if _, err := io.ReadFull(source, header); err != nil {
		return nil, ErrArchive
	}
	if !bytes.Equal(header[:8], []byte("PCBKP001")) || binary.BigEndian.Uint32(header[32:36]) != chunkBytes || binary.BigEndian.Uint32(header[36:40]) != keyIterations {
		return nil, ErrArchive
	}
	aead, err := archiveCipher(header, password)
	if err != nil {
		return nil, err
	}
	return &encryptedReader{ctx: ctx, source: source, header: header, aead: aead}, nil
}

func (r *encryptedReader) next() error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	frame := make([]byte, 9)
	if _, err := io.ReadFull(r.source, frame); err != nil {
		return ErrArchive
	}
	length := binary.BigEndian.Uint32(frame[4:8])
	if binary.BigEndian.Uint32(frame[:4]) != r.sequence || r.sequence >= uint32(MaxArchiveBytes/chunkBytes)+2 || frame[8] > 1 || length < uint32(r.aead.Overhead()) || length > uint32(chunkBytes+r.aead.Overhead()) {
		return ErrArchive
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(r.source, data); err != nil {
		return ErrArchive
	}
	nonce := make([]byte, 12)
	copy(nonce, r.header[24:32])
	binary.BigEndian.PutUint32(nonce[8:], r.sequence)
	aad := append(append([]byte(nil), r.header...), frame...)
	plaintext, err := r.aead.Open(data[:0], nonce, data, aad)
	if err != nil {
		return ErrArchive
	}
	r.sequence++
	r.bytes += int64(len(plaintext))
	if r.bytes > MaxArchiveBytes {
		return ErrArchive
	}
	if frame[8] == 1 {
		if len(plaintext) != 0 {
			return ErrArchive
		}
		var extra [1]byte
		if _, err := io.ReadFull(r.source, extra[:]); err != io.EOF {
			return ErrArchive
		}
		r.finished = true
		return io.EOF
	}
	if len(plaintext) == 0 {
		return ErrArchive
	}
	r.buffer = plaintext
	return nil
}

func (r *encryptedReader) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if len(r.buffer) == 0 {
		if r.finished {
			return 0, io.EOF
		}
		if err := r.next(); err != nil {
			return 0, err
		}
	}
	n := copy(data, r.buffer)
	r.buffer = r.buffer[n:]
	return n, nil
}

// gzip uses ByteReader to avoid consuming unverified trailing archive bytes.
func (r *encryptedReader) ReadByte() (byte, error) {
	var value [1]byte
	_, err := r.Read(value[:])
	return value[0], err
}
