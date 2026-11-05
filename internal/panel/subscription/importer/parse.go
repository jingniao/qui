package importer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

var ErrParse = errors.New("subscription has no supported valid proxies")

// Diagnostic field paths are named once here. The parsers pass these constants and
// the archive verifier accepts exactly this finite set (report.go), so a reason can
// never be stored with a path the verifier would refuse.
const (
	fieldNone        = ""
	fieldEntry       = "entry"
	fieldCredential  = "credential"
	fieldTLSInsecure = "tls.insecure"
	fieldMethod      = "method"
	fieldSecurity    = "security"
	fieldType        = "type"
	fieldNetwork     = "network"
	fieldWSOpts      = "ws-opts"
	fieldGRPCOpts    = "grpc-opts"
	fieldQuery       = "query"
)

type URI struct{}
type Base64 struct{}
type Clash struct{}

func (URI) Detect(b []byte) bool { return bytes.Contains(b, []byte("://")) }
func (Base64) Detect(b []byte) bool {
	decoded, err := decode64(string(bytes.TrimSpace(b)))
	return err == nil && (URI{}).Detect(decoded)
}
func (Clash) Detect(b []byte) bool { return bytes.Contains(b, []byte("proxies:")) }
func decode64(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, ErrParse
}

// looksLikeBase64 reports whether a document is a Base64 block that no decoder
// accepts. It is deliberately narrow: a document with markup, spaces, commas, a
// URI scheme or any other non-alphabet character is not Base64, and an alphabet-
// only run without a padding or a Base64-only character is more likely a word or a
// token than a subscription, so both stay "unrecognized" rather than gaining an
// invented cause.
func looksLikeBase64(b []byte) bool {
	trimmed := strings.TrimSpace(string(b))
	if len(trimmed) < 16 || strings.Contains(trimmed, "://") {
		return false
	}
	signal, compact := false, 0
	for _, char := range trimmed {
		switch {
		case char == '\r' || char == '\n' || char == ' ' || char == '\t':
			continue
		case char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9':
		case char == '=' || char == '+' || char == '/' || char == '-' || char == '_':
			signal = true
		default:
			return false
		}
		compact++
	}
	return signal && compact >= 16
}
func Parsers() []format.Parser { return []format.Parser{Clash{}, URI{}, Base64{}} }

// analyzer is the report-bearing half of every parser in this package, so a
// format has one implementation and not two: Parse is derived from analyze.
type analyzer interface {
	format.Parser
	analyze(content []byte, c *collector)
}

func analyzers() []analyzer {
	items := Parsers()
	out := make([]analyzer, 0, len(items))
	for _, item := range items {
		out = append(out, item.(analyzer))
	}
	return out
}

// Parse reports a generic diagnostic without echoing untrusted input, which may
// include a password, a token or the source's private subscription URL. It is
// Analyze without the report, kept for the format.Parser contract.
func Parse(b []byte) ([]proxy.Proxy, []proxy.Warning, error) {
	result, entries := Analyze(b)
	if result.Outcome == "failed" || len(entries) == 0 {
		return nil, nil, ErrParse
	}
	return entries, []proxy.Warning{proxy.SourceWarning("imported", "Only supported valid proxy entries are imported; unsupported fields and entries are omitted.")}, nil
}

// Analyze reads one fetched or pasted document and returns its accepted endpoints
// plus a bounded per-entry report. Counts are always determined once the document
// was read: total = accepted + rejected + duplicates, and an entry with several
// problems is counted once, under its first reason. The result's Message is always
// the fixed text of its code.
func Analyze(b []byte) (Result, []proxy.Proxy) {
	result, entries := analyze(b)
	result.Message = resultMessage(result.Code, result.Status)
	return result, entries
}

func analyze(b []byte) (Result, []proxy.Proxy) {
	result := Result{Diagnostics: []Diagnostic{}}
	if len(b) > MaxBody {
		result.Outcome, result.Stage, result.Code = "failed", stageDetect, "source_too_large"
		return result, nil
	}
	if len(bytes.TrimSpace(b)) == 0 {
		result.Outcome, result.Stage, result.Code = "failed", stageDetect, "import_empty"
		return result, nil
	}
	for _, parser := range analyzers() {
		if !parser.Detect(b) {
			continue
		}
		return runCollector(result, parser, b)
	}
	// No format claimed the document. A Base64 payload that no decoder accepts is
	// reported as an undecodable document instead of as an unknown format; text,
	// markup and anything that is not Base64 stays "unrecognized", so no category
	// is invented for it. The Base64 analyzer is the same one as above, so the
	// decode branch has one implementation.
	if looksLikeBase64(b) {
		return runCollector(result, Base64{}, b)
	}
	result.Outcome, result.Stage, result.Code = "failed", stageDetect, "import_unrecognized"
	return result, nil
}

// runCollector runs one parser's analysis and returns its report.
func runCollector(result Result, parser analyzer, content []byte) (Result, []proxy.Proxy) {
	c := &collector{result: &result, byContent: map[string]int{}, byKey: map[string]int{}}
	parser.analyze(content, c)
	c.finish()
	return result, c.proxies
}

// collector turns one document into accepted endpoints and reasons.
type collector struct {
	result     *Result
	proxies    []proxy.Proxy
	rejected   int
	duplicates int
	byContent  map[string]int
	byKey      map[string]int
	decided    bool
}

// documentFailure records a reason that belongs to the whole document, such as a
// Base64 payload that does not decode: no entry was read, so no count is set.
func (c *collector) documentFailure(stage, code string) {
	c.result.Outcome, c.result.Stage, c.result.Code, c.decided = "failed", stage, code, true
}

func (c *collector) reject(index int, protocol, code, field string) {
	c.rejected++
	c.result.add(entryDiagnostic(index, protocol, diagParse, code, field))
}

func (c *collector) duplicate(index int, protocol, code string, kept int) {
	c.duplicates++
	diagnostic := entryDiagnostic(index, protocol, diagDuplicate, code, fieldNone)
	diagnostic.KeptIndex = &kept
	c.result.add(diagnostic)
}

// entry counts one parsed entry exactly once. A later entry that repeats the
// content of an accepted one is reported as a content duplicate; the same
// name|protocol with different parameters keeps the first entry, because node
// groups and client policies address an imported endpoint by that key.
func (c *collector) entry(index int, protocol string, p proxy.Proxy, code, field string) {
	if code != "" {
		c.reject(index, protocol, code, field)
		return
	}
	raw, err := json.Marshal(p)
	if err != nil {
		c.reject(index, protocol, "oversized_entry", fieldNone)
		return
	}
	if kept, seen := c.byContent[string(raw)]; seen {
		c.duplicate(index, protocol, "duplicate_endpoint_content", kept)
		return
	}
	if kept, seen := c.byKey[p.Name+"|"+p.Protocol]; seen {
		c.duplicate(index, protocol, "duplicate_endpoint_key", kept)
		return
	}
	c.byContent[string(raw)] = index
	c.byKey[p.Name+"|"+p.Protocol] = index
	c.proxies = append(c.proxies, p)
}

// finish publishes the counts and the business outcome of the attempt.
func (c *collector) finish() {
	if c.decided {
		return
	}
	accepted, rejected, duplicates := len(c.proxies), c.rejected, c.duplicates
	total := accepted + rejected + duplicates
	c.result.Counts.Accepted = accepted
	c.result.Counts.Total, c.result.Counts.Rejected, c.result.Counts.Duplicates = &total, &rejected, &duplicates
	c.result.Stage = stageParse
	switch {
	case accepted == 0:
		c.result.Outcome, c.result.Code = "failed", "import_no_entries"
	case rejected+duplicates == 0:
		c.result.Outcome, c.result.Code = "ok", "import_ok"
	default:
		c.result.Outcome, c.result.Code = "partial", "import_partial"
	}
}
func valid(p proxy.Proxy) bool {
	code, _ := validation(p)
	return code == ""
}

// ValidateProxy applies the same semantic rules used before encrypted import
// persistence. Archive verification reuses it for newly supported endpoint
// settings instead of maintaining another cipher or key allowlist.
func ValidateProxy(p proxy.Proxy) error {
	if !valid(p) {
		return ErrParse
	}
	return nil
}

// validation reports the first rule that keeps a parsed proxy out of the cache,
// and the field it concerns. valid() is the same check without the reason, so the
// three parsers share one allowlist.
func validation(p proxy.Proxy) (string, string) {
	if code, field := proxy.ValidateClientSettings(p); code != "" {
		return code, field
	}
	// YAML escapes and aliases can expand during typed decoding. Bound the
	// canonical cache representation too, so every accepted import is backupable.
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > MaxBody {
		return "oversized_entry", ""
	}
	if len(p.Credential.Password) > 4096 {
		return "invalid_credential", fieldCredential
	}
	if p.TLS != nil && p.TLS.Insecure {
		return "insecure_tls", fieldTLSInsecure
	}
	if p.Protocol == "vless" && desiredstate.ValidateUUID(p.Credential.Password) != nil {
		return "invalid_uuid", fieldCredential
	}
	if p.Protocol == "tuic" && desiredstate.ValidateUUID(p.Credential.Username) != nil {
		return "invalid_uuid", fieldCredential
	}
	if p.Protocol == "shadowsocks" {
		if _, err := p.Shadowsocks(); err != nil {
			field := fieldCredential
			switch err {
			case proxy.ErrSSCipher:
				field = fieldMethod
			case proxy.ErrSSSecurity:
				field = fieldSecurity
			case proxy.ErrSSTransport:
				field = fieldType
			case proxy.ErrSSOption:
				field = fieldEntry
			}
			return err.Error(), field
		}
	}
	if p.Name == "" || len(p.Name) > 200 || p.Server == "" || len(p.Server) > 253 || p.Port < 1 || p.Port > 65535 {
		return "invalid_endpoint", fieldNone
	}
	if strings.ContainsAny(p.Server, " /\\\r\n\t@") || strings.IndexFunc(p.Name, unicode.IsControl) >= 0 {
		return "invalid_endpoint", fieldNone
	}
	switch p.Protocol {
	case "socks5", "http":
		return "", fieldNone
	case "vless", "trojan", "shadowsocks", "hysteria2", "tuic":
		if p.Credential.Password == "" {
			return "missing_credential", fieldCredential
		}
		return "", fieldNone
	}
	return "unsupported_protocol", fieldNone
}

// Parse keeps the format.Parser contract: the accepted endpoints of a document,
// or ErrParse when none survive.
func (URI) Parse(b []byte) ([]proxy.Proxy, error) {
	_, entries, err := parseWith(b, URI{})
	return entries, err
}

func (uri URI) analyze(content []byte, c *collector) {
	if len(content) > MaxBody {
		c.reject(0, fieldNone, "oversized_entry", fieldNone)
		return
	}
	index := 0
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p, code, field := uriProxy(line)
		c.entry(index, p.Protocol, p, code, field)
		index++
	}
}

// parseWith runs one parser's analysis and reports ErrParse when nothing was
// accepted, so Parse and Analyze never disagree about a document.
func parseWith(content []byte, parser format.Parser) (Result, []proxy.Proxy, error) {
	result, entries := Analyze(content)
	if !parser.Detect(content) || result.Outcome == "failed" || len(entries) == 0 {
		return result, nil, ErrParse
	}
	return result, entries, nil
}

// queryField names a rejected unknown query parameter only when it is a short
// safe token, so untrusted text never reaches the Owner's page.
func queryField(key string) string {
	if key == "" || len(key) > 32 {
		return fieldQuery
	}
	for _, char := range key {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return fieldQuery
		}
	}
	return "query." + strings.ToLower(key)
}

// uriProxy parses one URI line. It returns the proxy plus the reason it may not
// be cached, so the caller can report a precise diagnostic instead of dropping
// the line silently.
func uriProxy(line string) (proxy.Proxy, string, string) {
	// Tolerate a plain standard-Base64 SS password containing '/' on input.
	// net/url would otherwise interpret that byte as a path delimiter before it
	// sees '@'. Escaped input stays untouched; output always escapes this byte.
	if strings.HasPrefix(line, "ss://") {
		authority := strings.SplitN(strings.SplitN(line[5:], "#", 2)[0], "?", 2)[0]
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			line = "ss://" + strings.ReplaceAll(authority[:at], "/", "%2F") + line[5+at:]
		}
	}
	u, err := url.Parse(line)
	if err != nil || u.Hostname() == "" {
		return proxy.Proxy{}, "invalid_entry", ""
	}
	p := proxy.Proxy{Name: u.Fragment, Server: u.Hostname(), Protocol: u.Scheme}
	p.Port, _ = strconv.Atoi(u.Port())
	if p.Name == "" {
		p.Name = net.JoinHostPort(p.Server, u.Port())
	}
	if u.User != nil {
		p.Credential.Username = u.User.Username()
		p.Credential.Password, _ = u.User.Password()
	}
	q, queryErr := url.ParseQuery(u.RawQuery)
	if queryErr != nil {
		return p, "invalid_entry", fieldQuery
	}
	for key, values := range q {
		for _, value := range values[1:] {
			if value != values[0] {
				return p, "conflicting_alias", queryField(key)
			}
		}
	}
	for _, alias := range []string{"servername", "peer"} {
		if values, present := q[alias]; present {
			if sni, exists := q["sni"]; exists && sni[0] != values[0] {
				return p, "conflicting_alias", "tls.serverName"
			}
			q.Set("sni", values[0])
		}
	}
	switch p.Protocol {
	case "vless", "trojan", "hysteria2", "tuic":
		if u.User == nil {
			return p, "missing_credential", fieldCredential
		}
		p.Credential.Password = u.User.Username()
		if p.Protocol == "tuic" {
			p.Credential.Username = u.User.Username()
			p.Credential.Password, _ = u.User.Password()
		}
	case "ss":
		p.Protocol = "shadowsocks"
		if u.User == nil {
			return p, "missing_credential", fieldCredential
		}
		p.Method = p.Credential.Username
		if _, plain := u.User.Password(); !plain {
			decoded, e := decode64(p.Method)
			if e != nil {
				return p, "invalid_credential", fieldCredential
			}
			p.Method, p.Credential.Password, _ = strings.Cut(string(decoded), ":")
		}
		p.Method = strings.ToLower(p.Method)
		// The SS2022 userinfo's username is the cipher or its Base64 wrapper, not
		// an independent credential. Normalize it away so input encodings have
		// identical content identities in the encrypted cache.
		if proxy.SS2022(p.Method) {
			p.Credential.Username = ""
		}
		if proxy.SS2022(p.Method) && (u.RawQuery != "" || u.Path != "" && u.Path != "/") {
			return p, "unsupported_option", fieldEntry
		}
	case "socks":
		p.Protocol = "socks5"
	}
	for key := range q {
		switch key {
		case "extra":
			if len(q.Get(key)) > 4096 {
				return p, "oversized_entry", "transport.xhttp.extra"
			}
			return p, "unsupported_option", "transport.xhttp.extra"
		case "security", "sni", "peer", "servername", "alpn", "type", "path", "serviceName", "flow", "encryption", "pbk", "sid", "spx", "fp", "host", "mode":
		default:
			return p, "unsupported_transport", queryField(key)
		}
	}
	p.Flow = q.Get("flow")
	if encryption, present := q["encryption"]; present && (p.Protocol != "vless" || encryption[0] != "none") {
		return p, "invalid_encryption", "query.encryption"
	}
	p.Security = q.Get("security")
	if p.Protocol == "trojan" || p.Protocol == "hysteria2" || p.Protocol == "tuic" {
		p.Security = "tls"
	}
	if p.Security != "" && p.Security != "none" && p.Security != "tls" && p.Security != "reality" {
		return p, "unsupported_security", fieldSecurity
	}
	if p.Security == "tls" || p.Security == "reality" {
		sni := q.Get("sni")
		if sni == "" {
			sni = q.Get("peer")
		}
		p.TLS = &proxy.TLS{ServerName: sni, ClientFingerprint: q.Get("fp")}
		if q.Get("alpn") != "" {
			p.TLS.ALPN = strings.Split(q.Get("alpn"), ",")
		}
	}
	if p.Security == "reality" {
		p.Reality = &proxy.RealityOptions{PublicKey: q.Get("pbk")}
		if values, present := q["sid"]; present {
			value := strings.ToLower(values[0])
			p.Reality.ShortID = &value
		}
		if values, present := q["spx"]; present {
			value := values[0]
			p.Reality.SpiderX = &value
		}
	} else {
		for _, field := range []string{"pbk", "sid", "spx"} {
			if _, present := q[field]; present {
				return p, "invalid_protocol_combination", queryField(field)
			}
		}
		if _, present := q["fp"]; present && p.TLS == nil {
			return p, "invalid_protocol_combination", "tls.clientFingerprint"
		}
	}
	if transport := q.Get("type"); transport != "" && transport != "tcp" {
		if transport != "ws" && transport != "grpc" && transport != "xhttp" {
			return p, "unsupported_transport", fieldType
		}
		p.Transport = &proxy.Transport{Type: transport, Path: q.Get("path"), ServiceName: q.Get("serviceName")}
		if transport == "xhttp" {
			p.Transport.Path = ""
			p.Transport.XHTTP = &proxy.XHTTPOptions{Host: q.Get("host"), Path: q.Get("path"), Mode: q.Get("mode")}
		}
	}
	if q.Get("type") != "xhttp" {
		for _, field := range []string{"host", "mode"} {
			if _, present := q[field]; present {
				return p, "invalid_protocol_combination", queryField(field)
			}
		}
	}
	if p.Security == "reality" {
		transport := q.Get("type")
		if _, present := q["path"]; present && transport != "xhttp" {
			return p, "invalid_protocol_combination", "query.path"
		}
		if _, present := q["serviceName"]; present && transport != "grpc" {
			return p, "invalid_protocol_combination", "query.servicename"
		}
	}
	code, field := validation(p)
	if code != "" {
		return p, code, field
	}
	return p, "", ""
}
func (Base64) Parse(b []byte) ([]proxy.Proxy, error) {
	_, entries, err := parseWith(b, Base64{})
	return entries, err
}

// analyze decodes the document and then reads it as a URI list, so the two
// entry points cannot disagree about a single payload.
func (Base64) analyze(content []byte, c *collector) {
	if len(content) > MaxBody {
		c.reject(0, fieldNone, "oversized_entry", fieldNone)
		return
	}
	decoded, err := decode64(string(bytes.TrimSpace(content)))
	if err != nil {
		c.documentFailure(stageDecode, "invalid_base64")
		return
	}
	(URI{}).analyze(decoded, c)
}

type clashEntry struct {
	XHTTP *struct {
		Extra yaml.Node `yaml:"extra"`
		Host  string    `yaml:"host"`
		Path  string    `yaml:"path"`
		Mode  string    `yaml:"mode"`
	} `yaml:"xhttp-opts"`
	ClientFingerprint string `yaml:"client-fingerprint"`
	Fingerprint       string `yaml:"fingerprint"`
	Reality           *struct {
		PublicKey string  `yaml:"public-key"`
		ShortID   *string `yaml:"short-id"`
	} `yaml:"reality-opts"`
	Name       string   `yaml:"name"`
	Type       string   `yaml:"type"`
	Server     string   `yaml:"server"`
	Port       int      `yaml:"port"`
	Username   string   `yaml:"username"`
	Password   string   `yaml:"password"`
	UUID       string   `yaml:"uuid"`
	Cipher     string   `yaml:"cipher"`
	TLS        bool     `yaml:"tls"`
	SNI        string   `yaml:"sni"`
	ServerName string   `yaml:"servername"`
	ALPN       []string `yaml:"alpn"`
	Insecure   bool     `yaml:"skip-cert-verify"`
	UDP        bool     `yaml:"udp"`
	Network    string   `yaml:"network"`
	Flow       string   `yaml:"flow"`
	WS         *struct {
		Path string `yaml:"path"`
	} `yaml:"ws-opts"`
	GRPC *struct {
		Service string `yaml:"grpc-service-name"`
	} `yaml:"grpc-opts"`
}

func (Clash) Parse(b []byte) ([]proxy.Proxy, error) {
	_, entries, err := parseWith(b, Clash{})
	return entries, err
}

func (Clash) analyze(content []byte, c *collector) {
	if len(content) > MaxBody {
		c.reject(0, fieldNone, "oversized_entry", fieldNone)
		return
	}
	var document struct {
		Proxies []yaml.Node `yaml:"proxies"`
	}
	if yaml.Unmarshal(content, &document) != nil {
		c.documentFailure(stageDecode, "import_unrecognized")
		return
	}
	index := 0
	for _, node := range document.Proxies {
		p, code, field := clashProxy(node)
		c.entry(index, p.Protocol, p, code, field)
		index++
	}
}

// clashProxy decodes one `proxies` entry. Known-fields decoding keeps the
// allowlist strict: an option this build cannot represent rejects the entry with
// a named reason instead of being dropped silently.
func clashProxy(node yaml.Node) (proxy.Proxy, string, string) {
	raw, err := yaml.Marshal(&node)
	if err != nil {
		return proxy.Proxy{}, "invalid_entry", fieldNone
	}
	var entry clashEntry
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if decoder.Decode(&entry) != nil {
		return proxy.Proxy{}, "unsupported_option", fieldEntry
	}
	if entry.XHTTP != nil && entry.XHTTP.Extra.Kind != 0 {
		encoded, _ := yaml.Marshal(&entry.XHTTP.Extra)
		if len(encoded) > 4096 {
			return proxy.Proxy{}, "oversized_entry", "transport.xhttp.extra"
		}
		return proxy.Proxy{}, "unsupported_option", "transport.xhttp.extra"
	}
	p := proxy.Proxy{Name: entry.Name, Protocol: entry.Type, Server: entry.Server, Port: entry.Port, Method: entry.Cipher, Flow: entry.Flow, Credential: proxy.Credential{Username: entry.Username, Password: entry.Password}}
	if p.Protocol == "ss" {
		p.Protocol = "shadowsocks"
	}
	if p.Protocol == "shadowsocks" {
		p.Method = strings.ToLower(p.Method)
	}
	if p.Protocol == "vless" {
		p.Credential.Password = entry.UUID
	}
	if p.Protocol == "tuic" {
		p.Credential.Username = entry.UUID
	}
	if entry.TLS || entry.Reality != nil || p.Protocol == "trojan" || p.Protocol == "hysteria2" || p.Protocol == "tuic" {
		p.Security = "tls"
		sni := entry.SNI
		if sni == "" {
			sni = entry.ServerName
		}
		p.TLS = &proxy.TLS{ServerName: sni, ALPN: entry.ALPN, Insecure: entry.Insecure, ClientFingerprint: entry.ClientFingerprint, Fingerprint: entry.Fingerprint}
	}
	if entry.SNI != "" && entry.ServerName != "" && entry.SNI != entry.ServerName {
		return p, "conflicting_alias", "tls.serverName"
	}
	if entry.Reality != nil {
		if !entry.TLS {
			return p, "invalid_protocol_combination", "reality"
		}
		p.Security = "reality"
		p.Reality = &proxy.RealityOptions{PublicKey: entry.Reality.PublicKey, ShortID: entry.Reality.ShortID}
		if p.Reality.ShortID != nil {
			value := strings.ToLower(*p.Reality.ShortID)
			p.Reality.ShortID = &value
		}
	}
	if p.TLS == nil && (entry.ClientFingerprint != "" || entry.Fingerprint != "") {
		return p, "invalid_protocol_combination", "tls.clientFingerprint"
	}
	switch entry.Network {
	case "", "tcp":
	case "ws":
		if entry.WS == nil {
			return p, "unsupported_transport", fieldWSOpts
		}
		p.Transport = &proxy.Transport{Type: "ws", Path: entry.WS.Path}
	case "grpc":
		if entry.GRPC == nil {
			return p, "unsupported_transport", fieldGRPCOpts
		}
		p.Transport = &proxy.Transport{Type: "grpc", ServiceName: entry.GRPC.Service}
	case "xhttp":
		if entry.XHTTP == nil {
			return p, "invalid_xhttp_settings", "transport.xhttp"
		}
		p.Transport = &proxy.Transport{Type: "xhttp", XHTTP: &proxy.XHTTPOptions{Host: entry.XHTTP.Host, Path: entry.XHTTP.Path, Mode: entry.XHTTP.Mode}}
	default:
		return p, "unsupported_transport", fieldNetwork
	}
	if entry.XHTTP != nil && entry.Network != "xhttp" {
		return p, "invalid_protocol_combination", "transport.xhttp"
	}
	code, field := validation(p)
	if code != "" {
		return p, code, field
	}
	return p, "", fieldNone
}
