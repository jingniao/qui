package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
)

// The persisted refresh result is bounded: at most MaxDiagnostics reasons and at
// most MaxResultBytes of JSON. Everything below the limits is a count or a fixed
// message, never a raw URL, body, credential or library error.
const (
	MaxDiagnostics = 50
	MaxResultBytes = 32 << 10
)

// Diagnostic stages: a per-entry reason comes from parsing or from duplicate
// suppression.
const (
	diagParse     = "parse"
	diagDuplicate = "duplicate"
)

// Result stages name the layer that decided the attempt. They live here so the
// parser, the fetch layer, the store and the archive verifier share one list: a
// stored result can only name a stage this panel can produce.
const (
	stageInput    = "input"
	stageDNS      = "dns"
	stageAddress  = "address"
	stageRedirect = "redirect"
	stageTLS      = "tls"
	stageConnect  = "connect"
	stageDeadline = "deadline"
	stageHTTP     = "http"
	stageSize     = "size"
	stageDetect   = "detect"
	stageDecode   = "decode"
	stageParse    = "parse"
	stageCommit   = "commit"
	stageStore    = "store"
)

// diagnosticMessages is the complete, fixed Owner-facing text per code. Keeping
// the text here means a diagnostic can never carry input data: the archive
// verifier rejects a message that is not exactly the text of its code.
var diagnosticMessages = map[string]string{
	"invalid_ss2022_key_encoding": "SS2022 每段密钥必须使用标准带填充的 Base64 编码",
	"invalid_ss2022_key_length":   "SS2022 密钥长度无效：AES-128 需要 16 字节，AES-256 和 ChaCha20 需要 32 字节",
	"unsupported_ss2022_identity": "SS2022 仅支持单密钥或 AES 的两段身份密钥，不支持 ChaCha20 身份或更多密钥段",
	"invalid_entry":               "该行不是可识别的节点链接",
	"invalid_endpoint":            "节点名称、地址或端口无效",
	"missing_credential":          "缺少该协议必需的凭据",
	"invalid_uuid":                "凭据中的 UUID 格式无效",
	"invalid_credential":          "凭据格式无效",
	"unsupported_protocol":        "暂不支持该协议",
	"unsupported_cipher":          "暂不支持该加密方式",
	"unsupported_transport":       "暂不支持该传输方式",
	"unsupported_security":        "暂不支持该安全方式",
	"insecure_tls":                "已拒绝跳过证书校验的设置",
	"oversized_entry":             "该条目超出大小上限",
	"unsupported_option":          "该条目包含暂不支持的字段",
	"duplicate_endpoint_content":  "与前面的条目完全相同，已跳过",
	"duplicate_endpoint_key":      "同一来源内名称与协议重复，只保留第一条",
}

// diagnosticMessage returns the fixed text of a per-entry code.
func diagnosticMessage(code string) (string, bool) {
	message, ok := diagnosticMessages[code]
	return message, ok
}

// Diagnostic is one safe reason an input entry was not imported. EntryIndex is
// the entry's position in the fetched document, and KeptIndex names the entry
// that was kept when a later one repeated its name and protocol.
type Diagnostic struct {
	EntryIndex *int   `json:"entryIndex"`
	Protocol   string `json:"protocol,omitempty"`
	Stage      string `json:"stage"`
	Code       string `json:"code"`
	Field      string `json:"field,omitempty"`
	Message    string `json:"message"`
	KeptIndex  *int   `json:"keptEntryIndex,omitempty"`
}

// Counts describes one attempt. Total is accepted+rejected+duplicates once the
// document was read; a nil value means "not determined", never zero. That is also
// true for Cached, which is unknown when nothing could be read after a failure.
type Counts struct {
	Total      *int `json:"total"`
	Accepted   int  `json:"accepted"`
	Rejected   *int `json:"rejected"`
	Duplicates *int `json:"duplicates"`
	// Cached is the number of endpoints stored for the source after the attempt,
	// and Usable the number of them a subscription may use: a failed source keeps
	// its rows for diagnostics but contributes nothing, while a storage failure
	// leaves the previous committed count usable.
	Cached *int `json:"cached"`
	Usable *int `json:"usable"`
}

// Result is the structured outcome of one import attempt. It is what the Owner
// API returns and what subscription_sources.latest_result_json stores.
type Result struct {
	SourceID string `json:"sourceId"`
	Outcome  string `json:"outcome"`
	Stage    string `json:"stage"`
	Code     string `json:"code"`
	// Status is the upstream HTTP status the source answered with. Only a
	// source_http_status result carries one, and it is persisted with the result
	// so the API, the card and the dialog name the real status instead of zero.
	Status *int `json:"upstreamStatus,omitempty"`
	// Message is the fixed Owner-facing text of Code, produced by resultMessage and
	// verified against it, so no result can carry an arbitrary message.
	Message string `json:"message"`
	Counts  Counts `json:"counts"`
	// AttemptedAt is the latest attempt; LastSuccessAt stays unchanged when this
	// attempt failed.
	AttemptedAt   string       `json:"attemptedAt"`
	LastSuccessAt *string      `json:"lastSuccessAt"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
	// DiagnosticsTruncated reports that more reasons existed than the bounds keep.
	DiagnosticsTruncated bool `json:"diagnosticsTruncated"`
}

// add appends a diagnostic inside both bounds and drops the last ones if the
// serialized result would leave MaxResultBytes. Counts are never dropped.
func (r *Result) add(diagnostic Diagnostic) {
	if len(r.Diagnostics) >= MaxDiagnostics {
		r.DiagnosticsTruncated = true
		return
	}
	r.Diagnostics = append(r.Diagnostics, diagnostic)
	for len(r.Diagnostics) > 0 {
		raw, err := json.Marshal(r)
		if err == nil && len(raw) <= MaxResultBytes {
			return
		}
		r.Diagnostics = r.Diagnostics[:len(r.Diagnostics)-1]
		r.DiagnosticsTruncated = true
	}
}

// entryDiagnostic builds a bounded per-entry reason from fixed text.
func entryDiagnostic(index int, protocol, stage, code, field string) Diagnostic {
	message, _ := diagnosticMessage(code)
	return Diagnostic{EntryIndex: &index, Protocol: protocolName(protocol), Stage: stage, Code: code, Field: field, Message: message}
}

// protocolName keeps only a short lowercase scheme so an unknown or hostile
// scheme cannot smuggle text into the Owner's page or the log.
func protocolName(raw string) string {
	if raw == "" || len(raw) > 16 {
		return ""
	}
	for _, char := range raw {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
			return ""
		}
	}
	return raw
}

// resultSpec is what a result code may claim: the stages and the outcomes this
// panel can produce for it. The verifier refuses anything else, so a restored
// archive cannot present a stage or an outcome the panel would never write.
type resultSpec struct {
	stages   []string
	outcomes []string
}

var resultSpecs = map[string]resultSpec{
	"import_ok":                {[]string{stageParse}, []string{"ok"}},
	"import_partial":           {[]string{stageParse}, []string{"partial"}},
	"import_no_entries":        {[]string{stageParse}, []string{"failed"}},
	"import_unrecognized":      {[]string{stageDetect, stageDecode}, []string{"failed"}},
	"invalid_base64":           {[]string{stageDecode}, []string{"failed"}},
	"import_empty":             {[]string{stageDetect}, []string{"failed"}},
	"source_too_large":         {[]string{stageDetect, stageSize}, []string{"failed"}},
	"import_commit_failed":     {[]string{stageCommit}, []string{"failed"}},
	"invalid_source_url":       {[]string{stageInput}, []string{"failed"}},
	"source_dns_failed":        {[]string{stageDNS}, []string{"failed"}},
	"source_address_rejected":  {[]string{stageAddress}, []string{"failed"}},
	"source_redirect_rejected": {[]string{stageRedirect}, []string{"failed"}},
	"source_tls_failed":        {[]string{stageTLS}, []string{"failed"}},
	"source_connect_failed":    {[]string{stageConnect}, []string{"failed"}},
	"source_timeout":           {[]string{stageDeadline}, []string{"failed"}},
	"source_http_status":       {[]string{stageHTTP}, []string{"failed"}},
	// A pasted document the panel refused before any write, and a source whose own
	// stored payload cannot be read: both report the state that remains instead of
	// claiming that a source was fetched, excluded or changed.
	"import_paste_rejected":    {[]string{stageParse, stageDetect, stageDecode, stageSize}, []string{"failed"}},
	"import_paste_unchanged":   {[]string{stageParse, stageDetect, stageDecode, stageSize}, []string{"failed"}},
	"import_source_unreadable": {[]string{stageStore}, []string{"failed"}},
}

// fixedMessages is the complete, fixed Owner-facing text per code, so no result
// can carry a message that was not written here. source_http_status is formatted
// with its status instead (see resultMessage).
var fixedMessages = map[string]string{
	"import_ok":                "已导入全部可识别节点",
	"import_partial":           "部分条目无法导入，已导入其余条目",
	"import_no_entries":        "来源已抓取，但没有可导入的节点；该来源的节点已从订阅中排除",
	"import_unrecognized":      "无法识别订阅内容格式；该来源的节点已从订阅中排除",
	"invalid_base64":           "订阅内容看起来是 Base64，但无法解码；该来源的节点已从订阅中排除",
	"import_empty":             "订阅内容为空；该来源的节点已从订阅中排除",
	"import_commit_failed":     "导入结果未能保存，来源状态保持不变",
	"invalid_source_url":       "订阅链接必须是有效的 HTTPS 地址",
	"source_dns_failed":        "无法解析订阅来源的域名；该来源的节点已从订阅中排除",
	"source_address_rejected":  "订阅来源解析到非公网地址，已拒绝访问；该来源的节点已从订阅中排除",
	"source_redirect_rejected": "订阅来源的重定向被拒绝（必须是公网 HTTPS 地址，最多 10 跳）；该来源的节点已从订阅中排除",
	"source_tls_failed":        "订阅来源的 TLS 证书校验失败；该来源的节点已从订阅中排除",
	"source_connect_failed":    "无法连接订阅来源；该来源的节点已从订阅中排除",
	"source_timeout":           "抓取订阅来源超时（上限 15 秒）；该来源的节点已从订阅中排除",
	"source_too_large":         "订阅内容超过 2 MiB 上限；该来源的节点已从订阅中排除",
	// A paste refused before storage must not claim a fetch or an exclusion, and it
	// must not present the analysed entries as retained: nothing was written.
	"import_paste_rejected":  "粘贴内容没有可导入的节点，未保存任何来源",
	"import_paste_unchanged": "粘贴内容没有可导入的节点，未保存任何更改，现有来源状态保持不变",
	// A corrupted or foreign sealed payload is a local storage failure, not a fetch.
	"import_source_unreadable": "无法读取订阅来源的本地加密数据，来源状态保持不变",
}

// resultMessage is the single fixed formatter for a result code. The API, the
// stored last_error and the page all show its text, so a real upstream status is
// stated once and never replaced by zero. It returns "" for an unknown code.
func resultMessage(code string, status *int) string {
	if code == "source_http_status" {
		if status != nil && *status >= 100 && *status <= 599 {
			return "订阅来源返回 HTTP " + strconv.Itoa(*status) + "；该来源的节点已从订阅中排除"
		}
		return "订阅来源返回非预期的 HTTP 状态；该来源的节点已从订阅中排除"
	}
	return fixedMessages[code]
}

// statusPointer keeps an upstream status only when it is a real HTTP status, so
// no result ever states "HTTP 0".
func statusPointer(status int) *int {
	if status >= 100 && status <= 599 {
		return &status
	}
	return nil
}

func oneOf(value string, allowed []string) bool {
	for _, item := range allowed {
		if item == value {
			return true
		}
	}
	return false
}

// ValidateResult accepts only a result the panel itself could have written:
// known code, stage and outcome for that code, the fixed message of the code, RFC
// 3339 stamps, counts that agree with the entries and the committed cache, and
// diagnostics whose text, stage, index and field are the ones the parser emits. A
// backup or archive that fails this check is rejected instead of being displayed
// with an invented cause. Unknown JSON fields are refused too, so a foreign
// payload cannot ride along inside a valid result.
func ValidateResult(raw []byte) error {
	if len(raw) > MaxResultBytes {
		return ErrParse
	}
	var result Result
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return ErrParse
	}
	var extra any
	// Exactly one JSON value: a second value, or any trailing text that is not
	// whitespace, means this payload is not the one the panel wrote.
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrParse
	}
	spec, ok := resultSpecs[result.Code]
	if !ok || !oneOf(result.Outcome, spec.outcomes) || !oneOf(result.Stage, spec.stages) {
		return ErrParse
	}
	if result.Message == "" || result.Message != resultMessage(result.Code, result.Status) {
		return ErrParse
	}
	if result.Code != "source_http_status" {
		if result.Status != nil {
			return ErrParse
		}
	} else if result.Status == nil || *result.Status < 100 || *result.Status > 599 {
		return ErrParse
	}
	if !validStamp(result.AttemptedAt) || (result.LastSuccessAt != nil && !validStamp(*result.LastSuccessAt)) {
		return ErrParse
	}
	if result.Outcome != "failed" {
		// A committed attempt is its own latest success: the store writes both
		// stamps together, so an ok or partial result can never lack it or name
		// another moment.
		if result.LastSuccessAt == nil || !stampEqual(*result.LastSuccessAt, result.AttemptedAt) {
			return ErrParse
		}
	}
	if !validCounts(result) {
		return ErrParse
	}
	if len(result.Diagnostics) > MaxDiagnostics {
		return ErrParse
	}
	for _, item := range result.Diagnostics {
		if !validDiagnostic(result, item) {
			return ErrParse
		}
	}
	return nil
}

// diagnosticStages is the only stage a per-entry code may carry.
var diagnosticStages = map[string]string{
	"duplicate_endpoint_content": diagDuplicate,
	"duplicate_endpoint_key":     diagDuplicate,
}

// diagnosticFields are the fixed field paths the parsers emit; a rejected
// unknown query parameter is reported as query.<token> instead (queryField).
// Each entry is the named constant the parser itself passes (see parse.go), so a
// parser reason can never be stored with a path this verifier would refuse.
var diagnosticFields = map[string]bool{
	fieldNone: true, fieldCredential: true, fieldTLSInsecure: true, fieldMethod: true,
	fieldSecurity: true, fieldType: true, fieldQuery: true,
	fieldEntry: true, fieldWSOpts: true, fieldGRPCOpts: true, fieldNetwork: true,
}

// validField accepts only a fixed path or a bounded query token the parser itself
// emitted, so a diagnostic cannot carry a URL, a path or other untrusted text.
func validField(field string) bool {
	if diagnosticFields[field] {
		return true
	}
	token, ok := strings.CutPrefix(field, "query.")
	if !ok || token == "" || len(token) > 32 {
		return false
	}
	for _, char := range token {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

// validStamp accepts only the RFC3339 timestamps the panel writes.
func validStamp(value string) bool {
	if value == "" {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func stampEqual(first, second string) bool {
	a, err := time.Parse(time.RFC3339Nano, first)
	if err != nil {
		return false
	}
	b, err := time.Parse(time.RFC3339Nano, second)
	return err == nil && a.Equal(b)
}

// validCounts keeps the counts of an attempt consistent with what it did: total is
// the sum of its parts, committed endpoints are exactly what was accepted, a
// failed attempt retains nothing new, and usable never exceeds the cache.
func validCounts(result Result) bool {
	counts := result.Counts
	for _, value := range []*int{counts.Total, counts.Rejected, counts.Duplicates, counts.Cached, counts.Usable} {
		if value != nil && *value < 0 {
			return false
		}
	}
	if counts.Accepted < 0 {
		return false
	}
	if (counts.Total == nil) != (counts.Rejected == nil) || (counts.Rejected == nil) != (counts.Duplicates == nil) {
		return false
	}
	if counts.Total != nil && *counts.Total != counts.Accepted+*counts.Rejected+*counts.Duplicates {
		return false
	}
	if counts.Cached != nil && counts.Usable != nil && *counts.Usable > *counts.Cached {
		return false
	}
	if result.Outcome == "failed" {
		// A failed attempt commits nothing, so the endpoints it analysed are never
		// part of the cache: it reports what the source still holds, which is only a
		// usable cache when the attempt itself changed nothing.
		if counts.Accepted != 0 {
			return false
		}
		if keepsPreviousState(result) {
			// The counts describe the state that remains, so they are either both
			// known or both unknown.
			if (counts.Cached == nil) != (counts.Usable == nil) {
				return false
			}
		} else if counts.Usable != nil && *counts.Usable != 0 {
			return false
		}
		return true
	}
	if counts.Accepted <= 0 || counts.Total == nil || counts.Cached == nil || counts.Usable == nil ||
		*counts.Cached != counts.Accepted || *counts.Usable != counts.Accepted {
		return false
	}
	// A committed attempt is "ok" only when nothing was skipped, and "partial" only
	// when something was: an outcome that contradicts its own counts is not one the
	// parser produced.
	if result.Outcome == "ok" {
		return *counts.Rejected == 0 && *counts.Duplicates == 0
	}
	return *counts.Rejected+*counts.Duplicates > 0
}

// keepsPreviousState reports a failed result that changed nothing: a storage
// failure, a source whose own payload cannot be read, or a paste refused before any
// write. Its counts describe the state the source keeps.
func keepsPreviousState(result Result) bool {
	if result.Stage == stageCommit {
		return true
	}
	switch result.Code {
	case "import_source_unreadable", "import_paste_unchanged":
		return true
	}
	return false
}

// persistedResults are the codes the store writes to
// subscription_sources.latest_result_json: a paste that committed, or a fetch or
// parse failure that was recorded. A storage failure and a refused paste are
// reported to the Owner but never stored, so an archive cannot carry one.
var persistedResults = map[string]bool{
	"import_ok": true, "import_partial": true, "import_no_entries": true,
	"import_unrecognized": true, "invalid_base64": true, "import_empty": true,
	"source_too_large": true, "source_dns_failed": true, "source_address_rejected": true,
	"source_redirect_rejected": true, "source_tls_failed": true, "source_connect_failed": true,
	"source_timeout": true, "source_http_status": true,
}

func validDiagnostic(result Result, item Diagnostic) bool {
	message, ok := diagnosticMessage(item.Code)
	if !ok || message != item.Message {
		return false
	}
	stage := diagParse
	if duplicate, ok := diagnosticStages[item.Code]; ok {
		stage = duplicate
	}
	if item.Stage != stage || item.EntryIndex == nil || *item.EntryIndex < 0 || !validField(item.Field) {
		return false
	}
	if item.Protocol != protocolName(item.Protocol) {
		return false
	}
	if total := result.Counts.Total; total != nil && *item.EntryIndex >= *total {
		return false
	}
	if stage == diagDuplicate {
		// A duplicate names the earlier entry that was kept, and only a duplicate
		// carries a kept index.
		return item.KeptIndex != nil && *item.KeptIndex >= 0 && *item.KeptIndex < *item.EntryIndex && item.Field == ""
	}
	return item.KeptIndex == nil
}

// ValidateStoredResult checks a persisted result against the columns of its own
// source row, so an archive cannot attach a result to another source, claim an
// attempt time the row does not have, or present a failed source as successful.
func ValidateStoredResult(sourceID, lastFetchAt, lastSuccessAt, status string, raw []byte) error {
	if err := ValidateResult(raw); err != nil {
		return err
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return ErrParse
	}
	if sourceID == "" || result.SourceID != sourceID {
		return ErrParse
	}
	// Only a result the store actually writes can be persisted, and it always knows
	// the cache it left behind.
	if !persistedResults[result.Code] || result.Counts.Cached == nil || result.Counts.Usable == nil {
		return ErrParse
	}
	if !validStamp(lastFetchAt) || !stampEqual(result.AttemptedAt, lastFetchAt) {
		return ErrParse
	}
	if (lastSuccessAt == "") != (result.LastSuccessAt == nil) {
		return ErrParse
	}
	if lastSuccessAt != "" && !stampEqual(*result.LastSuccessAt, lastSuccessAt) {
		return ErrParse
	}
	switch status {
	case "ok":
		// A source is ok only when the last attempt committed endpoints.
		if result.Outcome == "failed" {
			return ErrParse
		}
		return nil
	case "failed":
		if result.Outcome != "failed" {
			return ErrParse
		}
		return nil
	}
	// A pending source has no result: its link changed and the previous attempt
	// belongs to the previous link.
	return ErrParse
}
