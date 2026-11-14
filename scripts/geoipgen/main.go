// Command geoipgen converts the DB-IP "IP to Country Lite" CSV into the
// compact range table embedded by internal/panel/geoip.
//
//	go run ./scripts/geoipgen -in dbip-country-lite-YYYY-MM.csv.gz -version YYYY-MM \
//	    -out internal/panel/geoip/country.bin.gz
//
// The CSV rows are "first,last,CC" and cover IPv4 then IPv6 in ascending order.
// Only range starts are stored: a gap becomes an unknown range, neighbours with
// the same country merge, and IPv6 keeps the upper 64 bits (no country is
// assigned below a /64 in practice; a split /64 keeps its first country).
//
// Layout before gzip, all integers unsigned varints:
//
//	"QGEO1\n" version-length version country-count {2-byte code}
//	v4-count {delta-from-previous-start country-index}
//	v6-count {delta-from-previous-start country-index}
//
// Index 0 is always "ZZ" (unknown). The first start of each family is 0.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"regexp"
	"sort"
	"strings"
)

const magic = "QGEO1\n"

type entry struct {
	start uint64
	index uint8
}

type family struct {
	entries []entry
	next    uint64 // first start not yet covered
	full    bool   // the whole family is covered
}

func (f *family) add(start, last uint64, index uint8) error {
	if f.full {
		return fmt.Errorf("range %d after the end of the family", start)
	}
	if start < f.next {
		if len(f.entries) > 0 && start == f.entries[len(f.entries)-1].start {
			// A split /64 (IPv6 only): keep the first country.
			if last >= f.next {
				f.advance(last)
			}
			return nil
		}
		if last < f.next {
			return nil // wholly inside an already covered /64
		}
		start = f.next
	}
	if start > f.next {
		f.push(f.next, 0)
	}
	f.push(start, index)
	f.advance(last)
	return nil
}

func (f *family) push(start uint64, index uint8) {
	if n := len(f.entries); n > 0 && f.entries[n-1].index == index {
		return
	}
	f.entries = append(f.entries, entry{start, index})
}

func (f *family) advance(last uint64) {
	if last == ^uint64(0) {
		f.full = true
		return
	}
	f.next = last + 1
}

func (f *family) finish(max uint64) {
	if !f.full && f.next <= max {
		f.push(f.next, 0)
	}
	if len(f.entries) == 0 || f.entries[0].start != 0 {
		f.entries = append([]entry{{0, 0}}, f.entries...)
	}
}

func main() {
	in := flag.String("in", "", "DB-IP country lite CSV (.csv or .csv.gz)")
	out := flag.String("out", "", "output file")
	version := flag.String("version", "", "data release, e.g. 2026-11")
	flag.Parse()
	if *in == "" || *out == "" || !regexp.MustCompile(`^\d{4}-\d{2}$`).MatchString(*version) {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*in, *out, *version); err != nil {
		fmt.Fprintln(os.Stderr, "geoipgen:", err)
		os.Exit(1)
	}
}

func run(inPath, outPath, version string) error {
	file, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer file.Close()
	var reader io.Reader = file
	if strings.HasSuffix(inPath, ".gz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	}

	codes := []string{"ZZ"}
	indexOf := map[string]uint8{"ZZ": 0}
	var v4, v6 family
	lines := bufio.NewScanner(reader)
	lines.Buffer(make([]byte, 4096), 4096)
	line := 0
	for lines.Scan() {
		line++
		fields := strings.Split(strings.TrimSpace(lines.Text()), ",")
		if len(fields) != 3 {
			return fmt.Errorf("line %d: want 3 fields", line)
		}
		first, err1 := netip.ParseAddr(fields[0])
		last, err2 := netip.ParseAddr(fields[1])
		code := fields[2]
		if err1 != nil || err2 != nil || !regexp.MustCompile(`^[A-Z]{2}$`).MatchString(code) {
			return fmt.Errorf("line %d: malformed row", line)
		}
		index, ok := indexOf[code]
		if !ok {
			if len(codes) == 255 {
				return errors.New("more than 255 country codes")
			}
			index = uint8(len(codes))
			indexOf[code] = index
			codes = append(codes, code)
		}
		switch {
		case first.Is4() && last.Is4():
			a, b := first.As4(), last.As4()
			s, e := uint64(binary.BigEndian.Uint32(a[:])), uint64(binary.BigEndian.Uint32(b[:]))
			if e < s || len(v6.entries) > 0 {
				return fmt.Errorf("line %d: IPv4 range out of order", line)
			}
			if err := v4.add(s, e, index); err != nil {
				return fmt.Errorf("line %d: %w", line, err)
			}
			if e == 0xFFFFFFFF {
				v4.full = true
			}
		case first.Is6() && last.Is6() && !first.Is4In6():
			a, b := first.As16(), last.As16()
			s, e := binary.BigEndian.Uint64(a[:8]), binary.BigEndian.Uint64(b[:8])
			if e < s {
				return fmt.Errorf("line %d: IPv6 range out of order", line)
			}
			if err := v6.add(s, e, index); err != nil {
				return fmt.Errorf("line %d: %w", line, err)
			}
		default:
			return fmt.Errorf("line %d: mixed address families", line)
		}
	}
	if err := lines.Err(); err != nil {
		return err
	}
	v4.finish(0xFFFFFFFF)
	v6.finish(^uint64(0))
	for _, f := range []*family{&v4, &v6} {
		if !sort.SliceIsSorted(f.entries, func(i, j int) bool { return f.entries[i].start < f.entries[j].start }) {
			return errors.New("entries not sorted")
		}
	}

	var raw bytes.Buffer
	raw.WriteString(magic)
	putUvarint(&raw, uint64(len(version)))
	raw.WriteString(version)
	putUvarint(&raw, uint64(len(codes)))
	for _, code := range codes {
		raw.WriteString(code)
	}
	for _, f := range []*family{&v4, &v6} {
		putUvarint(&raw, uint64(len(f.entries)))
		previous := uint64(0)
		for i, item := range f.entries {
			delta := item.start - previous
			if i > 0 && delta == 0 {
				return errors.New("duplicate start")
			}
			putUvarint(&raw, delta)
			raw.WriteByte(item.index)
			previous = item.start
		}
	}

	var packed bytes.Buffer
	gz, err := gzip.NewWriterLevel(&packed, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := gz.Write(raw.Bytes()); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, packed.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("version %s: %d countries, %d IPv4 and %d IPv6 ranges, %d bytes raw, %d bytes gzip\n",
		version, len(codes)-1, len(v4.entries), len(v6.entries), raw.Len(), packed.Len())
	return nil
}

func putUvarint(buffer *bytes.Buffer, value uint64) {
	var scratch [binary.MaxVarintLen64]byte
	buffer.Write(scratch[:binary.PutUvarint(scratch[:], value)])
}
