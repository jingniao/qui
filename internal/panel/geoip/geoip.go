// Package geoip maps a public IP address to its ISO 3166-1 alpha-2 country
// code from an offline table embedded in the panel binary, so the server list
// can show a flag without sending node addresses to any outside service.
//
// The table is generated from DB-IP "IP to Country Lite" (CC BY 4.0,
// https://db-ip.com) by scripts/geoipgen; see LICENSE-DB-IP.md. It is decoded
// on first use (about 5 MiB in memory) and never refreshed at run time: a newer
// table arrives with a newer panel release.
package geoip

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"sync"

	"proxycontrol/pkg/contracts/netaddr"
)

//go:embed country.bin.gz
var packed []byte

const magic = "QGEO1\n"

type table struct {
	version string
	codes   []string
	v4      []uint32
	v4Index []uint8
	v6      []uint64
	v6Index []uint8
}

var (
	loadOnce sync.Once
	loaded   *table
	loadErr  error
)

func current() (*table, error) {
	loadOnce.Do(func() { loaded, loadErr = decode(packed) })
	return loaded, loadErr
}

// Country returns the upper-case country code for a public address (the
// netaddr.Public rule the Agent also applies to what it reports), or "" when
// the address is empty, malformed, not public, or unassigned in the table.
func Country(address string) string {
	addr, err := netip.ParseAddr(address)
	if err != nil || addr.Zone() != "" {
		return ""
	}
	addr = addr.Unmap()
	if !netaddr.Public(net.IP(addr.AsSlice())) {
		return ""
	}
	t, err := current()
	if err != nil {
		return ""
	}
	var index uint8
	if addr.Is4() {
		raw := addr.As4()
		key := binary.BigEndian.Uint32(raw[:])
		i := sort.Search(len(t.v4), func(i int) bool { return t.v4[i] > key }) - 1
		if i < 0 {
			return ""
		}
		index = t.v4Index[i]
	} else {
		raw := addr.As16()
		key := binary.BigEndian.Uint64(raw[:8])
		i := sort.Search(len(t.v6), func(i int) bool { return t.v6[i] > key }) - 1
		if i < 0 {
			return ""
		}
		index = t.v6Index[i]
	}
	if index == 0 {
		return ""
	}
	return t.codes[index]
}

// Version is the data release embedded in this binary (for example "2026-11"),
// or "" when the table cannot be read.
func Version() string {
	t, err := current()
	if err != nil {
		return ""
	}
	return t.version
}

// Err reports a damaged embedded table; Country then always returns "".
func Err() error {
	_, err := current()
	return err
}

func decode(data []byte) (*table, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("geoip: %w", err)
	}
	raw, err := io.ReadAll(io.LimitReader(gz, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("geoip: %w", err)
	}
	r := &reader{data: raw}
	if !bytes.HasPrefix(raw, []byte(magic)) {
		return nil, errors.New("geoip: unknown table format")
	}
	r.offset = len(magic)
	t := &table{}
	t.version = string(r.bytes(r.uvarint(64)))
	count := r.uvarint(255)
	if r.err == nil && count == 0 {
		r.err = errors.New("no countries")
	}
	for i := uint64(0); i < count && r.err == nil; i++ {
		code := r.bytes(2)
		if len(code) == 2 && (code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z') {
			r.err = errors.New("invalid country code")
		}
		t.codes = append(t.codes, string(code))
	}
	readFamily := func(max uint64) ([]uint64, []uint8) {
		n := r.uvarint(uint64(len(raw)))
		starts := make([]uint64, 0, n)
		indexes := make([]uint8, 0, n)
		previous := uint64(0)
		for i := uint64(0); i < n && r.err == nil; i++ {
			delta := r.uvarint(^uint64(0))
			index := r.byte()
			if r.err != nil {
				break
			}
			start := previous + delta
			switch {
			case i == 0 && delta != 0:
				r.err = errors.New("range table does not start at zero")
			case i > 0 && (delta == 0 || start < previous || start > max):
				r.err = errors.New("range table out of order")
			case uint64(index) >= count:
				r.err = errors.New("country index out of range")
			}
			starts = append(starts, start)
			indexes = append(indexes, index)
			previous = start
		}
		return starts, indexes
	}
	v4, v4Index := readFamily(0xFFFFFFFF)
	v6, v6Index := readFamily(^uint64(0))
	if r.err == nil && r.offset != len(raw) {
		r.err = errors.New("trailing data")
	}
	if r.err == nil && (len(v4) == 0 || len(v6) == 0) {
		r.err = errors.New("empty range table")
	}
	if r.err != nil {
		return nil, fmt.Errorf("geoip: %w", r.err)
	}
	t.v4 = make([]uint32, len(v4))
	for i, start := range v4 {
		t.v4[i] = uint32(start)
	}
	t.v4Index, t.v6, t.v6Index = v4Index, v6, v6Index
	return t, nil
}

type reader struct {
	data   []byte
	offset int
	err    error
}

func (r *reader) uvarint(max uint64) uint64 {
	if r.err != nil {
		return 0
	}
	value, n := binary.Uvarint(r.data[r.offset:])
	if n <= 0 {
		r.err = errors.New("truncated table")
		return 0
	}
	if value > max {
		r.err = errors.New("value out of range")
		return 0
	}
	r.offset += n
	return value
}

func (r *reader) byte() uint8 {
	if r.err != nil {
		return 0
	}
	if r.offset >= len(r.data) {
		r.err = errors.New("truncated table")
		return 0
	}
	value := r.data[r.offset]
	r.offset++
	return value
}

func (r *reader) bytes(n uint64) []byte {
	if r.err != nil {
		return nil
	}
	if uint64(len(r.data)-r.offset) < n {
		r.err = errors.New("truncated table")
		return nil
	}
	value := r.data[r.offset : r.offset+int(n)]
	r.offset += int(n)
	return value
}
