package testutil

import (
	"bufio"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// GateSource selects a real non-loopback source because online observation
// deliberately excludes loopback. It is called only by env-gated core tests.
func GateSource(t *testing.T) net.IP {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range addrs {
		if n, ok := value.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsGlobalUnicast() && !n.IP.IsLoopback() {
			return n.IP.To4()
		}
	}
	t.Fatal("real-core gate test requires a non-loopback IPv4 interface")
	return nil
}
func GateEcho(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return l.Addr().String()
}
func GateTunnel(proxy, target, user, password string, source net.IP) (net.Conn, error) {
	d := net.Dialer{Timeout: time.Second}
	if source != nil {
		d.LocalAddr = &net.TCPAddr{IP: source}
	}
	c, err := d.Dial("tcp", proxy)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = io.WriteString(c, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\nProxy-Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password))+"\r\n\r\n")
	if err != nil {
		c.Close()
		return nil, err
	}
	r, err := http.ReadResponse(bufio.NewReader(c), &http.Request{Method: http.MethodConnect})
	if err != nil {
		c.Close()
		return nil, err
	}
	if r.StatusCode != 200 {
		c.Close()
		return nil, io.ErrUnexpectedEOF
	}
	return c, nil
}
func GateRoundTrip(c net.Conn) bool {
	if c == nil {
		return false
	}
	c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write([]byte{42}); err != nil {
		return false
	}
	b := []byte{0}
	_, err := io.ReadFull(c, b)
	return err == nil && b[0] == 42
}
