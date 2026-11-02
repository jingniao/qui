package gost

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The two cumulative byte counters GOST exposes per service. They cover TCP and
// UDP forwards, reset with the process and carry `client`, `host` and `service`
// labels; only `service` is used, because the other two would multiply the series
// by every client address.
const (
	inputMetric  = "gost_service_transfer_input_bytes_total"
	outputMetric = "gost_service_transfer_output_bytes_total"
)

// The answer is streamed and bounded: 123 lines were measured for a two-service
// node, and a series appears only after that service carried traffic.
const (
	maxMetricsBytes = 1 << 20
	maxMetricsLines = 65536
	maxMetricBytes  = 1 << 62
)

// readMetrics fetches the Prometheus text from the private unix socket and sums
// both transfer counters over the given services.
func readMetrics(ctx context.Context, socket string, services map[string]bool) (uint64, uint64, error) {
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://gost/metrics", nil)
	if err != nil {
		return 0, 0, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return 0, 0, ctx.Err()
		}
		return 0, 0, errors.New("gost metrics socket unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, 0, errors.New("gost metrics socket rejected the request")
	}
	return parseMetrics(io.LimitReader(response.Body, maxMetricsBytes), services)
}

// parseMetrics streams the Prometheus text exposition and returns the summed input
// and output bytes of the named services. Series of other metrics and other
// services are skipped; a malformed line is an error rather than a silently
// smaller total.
func parseMetrics(reader io.Reader, services map[string]bool) (uint64, uint64, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), 64<<10)
	var upload, download uint64
	lines := 0
	for scanner.Scan() {
		lines++
		if lines > maxMetricsLines {
			return 0, 0, errors.New("gost metrics answer is too long")
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, err := parseSeries(line)
		if err != nil {
			return 0, 0, err
		}
		if name != inputMetric && name != outputMetric || !services[labels["service"]] {
			continue
		}
		count, err := parseCount(value)
		if err != nil {
			return 0, 0, err
		}
		target := &upload
		if name == outputMetric {
			target = &download
		}
		if *target > math.MaxUint64-count {
			return 0, 0, errors.New("gost metrics total overflow")
		}
		*target += count
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, errors.New("gost metrics answer is unreadable")
	}
	return upload, download, nil
}

// parseSeries splits one exposition line into its metric name, its labels and its
// value. Label values are unescaped, so a service name containing a quote or a
// backslash still matches the rendered one.
func parseSeries(line string) (string, map[string]string, string, error) {
	open := strings.IndexByte(line, '{')
	if open < 0 {
		name, rest, _ := strings.Cut(line, " ")
		value, err := parseValue(rest)
		return name, nil, value, err
	}
	name := strings.TrimSpace(line[:open])
	if name == "" {
		return "", nil, "", errors.New("gost metrics series has no metric name")
	}
	labels := map[string]string{}
	rest := line[open+1:]
	for {
		rest = strings.TrimLeft(rest, " ")
		if rest == "" {
			return "", nil, "", errors.New("gost metrics series has an unterminated label set")
		}
		if rest[0] == '}' {
			rest = rest[1:]
			break
		}
		equals := strings.IndexByte(rest, '=')
		if equals <= 0 {
			return "", nil, "", errors.New("gost metrics series has a malformed label")
		}
		key := strings.TrimSpace(rest[:equals])
		rest = rest[equals+1:]
		if rest == "" || rest[0] != '"' {
			return "", nil, "", errors.New("gost metrics series has an unquoted label value")
		}
		var value strings.Builder
		index := 1
		for ; index < len(rest); index++ {
			character := rest[index]
			if character == '\\' && index+1 < len(rest) {
				index++
				value.WriteByte(unescape(rest[index]))
				continue
			}
			if character == '"' {
				break
			}
			value.WriteByte(character)
		}
		if index >= len(rest) {
			return "", nil, "", errors.New("gost metrics series has an unterminated label value")
		}
		labels[key] = value.String()
		rest = strings.TrimLeft(rest[index+1:], " ")
		if rest == "" {
			return "", nil, "", errors.New("gost metrics series has an unterminated label set")
		}
		if rest[0] == ',' {
			rest = rest[1:]
		}
	}
	value, err := parseValue(rest)
	return name, labels, value, err
}

// parseValue reads the value of a series and drops an optional timestamp.
func parseValue(rest string) (string, error) {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", errors.New("gost metrics series has no value")
	}
	if len(fields) > 2 {
		return "", errors.New("gost metrics series has trailing data")
	}
	return fields[0], nil
}

func unescape(character byte) byte {
	switch character {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	default:
		return character
	}
}

// parseCount reads one Prometheus value, which is a float that the exporter also
// prints in scientific notation for a large counter.
func parseCount(value string) (uint64, error) {
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > maxMetricBytes {
		return 0, errors.New("gost metrics value is not a byte count")
	}
	return uint64(math.Round(number)), nil
}
