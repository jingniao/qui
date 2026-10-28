package testutil

import (
	"encoding/binary"
	"io"
	"net"
	"time"
)

// GateUDPExchange creates a fresh SOCKS UDP association and returns only whether
// its echo arrived. Closing the control socket ends this association between
// baseline and gated assertions, so an established flow cannot mask a failure.
func GateUDPExchange(proxy, target string) bool {
	c, err := net.DialTimeout("tcp", proxy, time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	c.Write([]byte{5, 1, 0})
	r := make([]byte, 2)
	if _, err := io.ReadFull(c, r); err != nil || r[1] != 0 {
		return false
	}
	c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0})
	head := make([]byte, 4)
	if _, err := io.ReadFull(c, head); err != nil || head[1] != 0 {
		return false
	}
	n := 0
	switch head[3] {
	case 1:
		n = 4
	case 4:
		n = 16
	default:
		return false
	}
	tail := make([]byte, n+2)
	if _, err := io.ReadFull(c, tail); err != nil {
		return false
	}
	address := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(binary.BigEndian.Uint16(tail[n:]))}
	udp, err := net.DialUDP("udp", nil, address)
	if err != nil {
		return false
	}
	defer udp.Close()
	udp.SetDeadline(time.Now().Add(time.Second))
	dest, err := net.ResolveUDPAddr("udp", target)
	if err != nil {
		return false
	}
	packet := append([]byte{0, 0, 0, 1}, dest.IP.To4()...)
	packet = binary.BigEndian.AppendUint16(packet, uint16(dest.Port))
	packet = append(packet, 42)
	if _, err := udp.Write(packet); err != nil {
		return false
	}
	b := make([]byte, 1024)
	count, err := udp.Read(b)
	return err == nil && count > 10 && b[count-1] == 42
}
