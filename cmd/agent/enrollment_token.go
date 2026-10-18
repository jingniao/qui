package main

import (
	"errors"
	"io"
	"os"
	"proxycontrol/internal/platform/filelock"
	"strings"
)

func readEnrollmentToken(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 512 {
		return "", errors.New("enrollment token must be a bounded regular local file")
	}
	if os.PathSeparator != '\\' && info.Mode().Perm()&0077 != 0 {
		return "", errors.New("enrollment token file must be accessible only to its owner")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || filelock.CheckSingleLink(file) != nil {
		return "", errors.New("enrollment token file changed or has multiple links")
	}
	data, err := io.ReadAll(io.LimitReader(file, 513))
	if err != nil {
		return "", err
	}
	defer clear(data)
	if len(data) > 512 {
		return "", errors.New("enrollment token exceeds bounds")
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 1 || len(token) > 256 || strings.ContainsAny(token, "\r\n\t\x00") {
		return "", errors.New("invalid enrollment token file")
	}
	return token, nil
}
