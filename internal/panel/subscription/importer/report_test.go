package importer

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Counts must agree with the entries: total = accepted + rejected + duplicates,
// a repeated name|protocol keeps the first entry, and an identical entry is a
// content duplicate rather than a key duplicate.
func TestAnalyzeCountsAndDuplicates(t *testing.T) {
	const uuid = "11111111-1111-1111-1111-111111111111"
	content := strings.Join([]string{
		"socks5://fixture:fixture@a.example.com:1080#A",
		"socks5://fixture:fixture@a.example.com:1080#A",
		"vless://" + uuid + "@b.example.com:443?security=tls&type=grpc&serviceName=svc#Same",
		"vless://" + uuid + "@b.example.com:443?security=tls&type=ws&path=%2Fws#Same",
		"vmess://unsupported.example.com:443",
	}, "\n")
	result, entries := Analyze([]byte(content))
	if len(entries) != 2 || result.Outcome != "partial" || result.Code != "import_partial" {
		t.Fatalf("entries=%d result=%+v", len(entries), result)
	}
	counts := result.Counts
	if *counts.Total != 5 || counts.Accepted != 2 || *counts.Rejected != 1 || *counts.Duplicates != 2 {
		t.Fatalf("counts: %+v", counts)
	}
	if len(result.Diagnostics) != 3 {
		t.Fatalf("diagnostics: %+v", result.Diagnostics)
	}
	if d := result.Diagnostics[0]; d.Code != "duplicate_endpoint_content" || *d.EntryIndex != 1 || d.KeptIndex == nil || *d.KeptIndex != 0 || d.Stage != diagDuplicate {
		t.Fatalf("content duplicate: %+v", d)
	}
	if d := result.Diagnostics[1]; d.Code != "duplicate_endpoint_key" || *d.EntryIndex != 3 || d.KeptIndex == nil || *d.KeptIndex != 2 || d.Protocol != "vless" {
		t.Fatalf("key duplicate: %+v", d)
	}
	if d := result.Diagnostics[2]; d.Code != "unsupported_protocol" || *d.EntryIndex != 4 || d.Stage != diagParse {
		t.Fatalf("rejection: %+v", d)
	}
	// A repeated identical entry is never stored twice, so a saved reference keeps
	// pointing at the first one.
	if entries[0].Name != "A" || entries[1].Name != "Same" {
		t.Fatalf("kept entries: %+v", entries)
	}
	// Explicit empty diagnoses are recorded for a clean document.
	clean, cleanEntries := Analyze([]byte("socks5://fixture:fixture@a.example.com:1080#A"))
	if clean.Outcome != "ok" || clean.Code != "import_ok" || *clean.Counts.Total != 1 || len(cleanEntries) != 1 || len(clean.Diagnostics) != 0 || clean.DiagnosticsTruncated {
		t.Fatalf("clean document: %+v", clean)
	}
}

// The report is bounded: more reasons than MaxDiagnostics keep the first ones and
// set the truncation flag instead of growing without limit.
func TestAnalyzeBoundsDiagnostics(t *testing.T) {
	lines := []string{}
	for index := 0; index < MaxDiagnostics+10; index++ {
		lines = append(lines, "vmess://unsupported"+string(rune('a'+index%26))+".example.com:443")
	}
	result, entries := Analyze([]byte(strings.Join(lines, "\n")))
	if len(entries) != 0 || result.Outcome != "failed" || result.Code != "import_no_entries" {
		t.Fatalf("result=%+v entries=%d", result, len(entries))
	}
	if *result.Counts.Total != MaxDiagnostics+10 || len(result.Diagnostics) != MaxDiagnostics || !result.DiagnosticsTruncated {
		t.Fatalf("diagnostics=%d truncated=%t total=%v", len(result.Diagnostics), result.DiagnosticsTruncated, *result.Counts.Total)
	}
	if raw, err := json.Marshal(result); err != nil || len(raw) > MaxResultBytes {
		t.Fatalf("serialized result %d bytes: %v", len(raw), err)
	}
}

// A document-level failure reports no counts, because no entry was read.
func TestAnalyzeDocumentLevelFailures(t *testing.T) {
	for _, test := range []struct {
		content string
		stage   string
		code    string
	}{
		{"", "detect", "import_empty"},
		{"   \n\t", "detect", "import_empty"},
		{"hello, this is not a subscription", "detect", "import_unrecognized"},
	} {
		result, entries := Analyze([]byte(test.content))
		if result.Outcome != "failed" || result.Stage != test.stage || result.Code != test.code || result.Counts.Total != nil || result.Counts.Rejected != nil || len(entries) != 0 {
			t.Fatalf("%q: %+v", test.content, result)
		}
	}
}

// A Base64 payload that no decoder accepts is reported as an undecodable document,
// while text, markup and a plain word stay "unrecognized": the analyzer never
// invents a category for arbitrary input.
func TestAnalyzeBase64Lookalike(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString([]byte("socks5://fixture:fixture@example.com:1080#cached"))
	if result, entries := Analyze([]byte(valid)); result.Code != "import_ok" || len(entries) != 1 {
		t.Fatalf("valid Base64: %+v (%d entries)", result, len(entries))
	}
	// A padding byte inside the payload defeats every Base64 decoder.
	malformed := valid[:8] + "=" + valid[8:]
	result, entries := Analyze([]byte(malformed))
	if result.Outcome != "failed" || result.Stage != stageDecode || result.Code != "invalid_base64" || len(entries) != 0 || result.Counts.Total != nil {
		t.Fatalf("malformed Base64: %+v", result)
	}
	if result.Message == "" {
		t.Fatal("malformed Base64 has no fixed message")
	}
	for _, text := range []string{"hello, this is not a subscription", "<html><body>please log in</body></html>", "thisisnotasubscriptiondocument"} {
		if result, _ := Analyze([]byte(text)); result.Code != "import_unrecognized" || result.Stage != stageDetect {
			t.Fatalf("%q: %+v", text, result)
		}
	}
}

// Every result the panel produces passes the shared verifier, including the commit
// and fetch failures that are reported to the Owner but never stored.
func TestValidateResultAcceptsPanelResults(t *testing.T) {
	stamp := "2026-11-05T00:00:00Z"
	const mixed = "socks5://fixture:fixture@a.example.com:1080#A\nvmess://unsupported.example.com:443"
	analyzed := func(content string) Result {
		result, _ := Analyze([]byte(content))
		result.SourceID, result.AttemptedAt = "source", stamp
		if result.Outcome != "failed" {
			// The store records what it committed, so a committed attempt can use
			// exactly what it accepted.
			result.LastSuccessAt = &stamp
			result.Counts.Cached, result.Counts.Usable = pointer(result.Counts.Accepted), pointer(result.Counts.Accepted)
		}
		return result
	}
	for _, test := range []struct {
		name string
		body func() Result
	}{
		{"ok", func() Result { return analyzed("socks5://fixture:fixture@a.example.com:1080#A") }},
		{"partial", func() Result { return analyzed(mixed) }},
		{"failed parse", func() Result { return analyzed("vmess://unsupported.example.com:443") }},
		{"failed unrecognized", func() Result { return analyzed("hello, this is not a subscription") }},
		{"commit", func() Result {
			result, _ := commitFailure("source", stamp, committedSource{known: true, status: "ok", cached: 2, lastSuccess: &stamp}, ErrParse)
			return result
		}},
		{"commit without a committed cache", func() Result {
			result, _ := commitFailure("source", stamp, committedSource{}, ErrParse)
			return result
		}},
		{"fetch http", func() Result {
			result := failureResult(stamp, "source", committedSource{}, stageHTTP, "source_http_status")
			result.Status = statusPointer(403)
			result.Message = resultMessage(result.Code, result.Status)
			return result
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := test.body()
			raw, err := json.Marshal(result)
			if err != nil || ValidateResult(raw) != nil {
				t.Fatalf("own result rejected: %+v %v", result, err)
			}
		})
	}
}

// The verifier refuses a result the panel could not have written: a stage or outcome
// the code never has, a text that is not the fixed message, a non-RFC3339 stamp, a
// status on the wrong code, counts that contradict the entries, a field or index the
// parser never emits, and an unknown JSON key.
func TestValidateResultRefusesForeignResults(t *testing.T) {
	stamp := "2026-11-05T00:00:00Z"
	valid := func() Result {
		result, _ := Analyze([]byte("socks5://fixture:fixture@a.example.com:1080#A\nvmess://unsupported.example.com:443\nvmess://unsupported.example.com:443"))
		result.SourceID, result.AttemptedAt, result.LastSuccessAt = "source", stamp, &stamp
		result.Counts.Cached, result.Counts.Usable = pointer(result.Counts.Accepted), pointer(result.Counts.Accepted)
		return result
	}
	for _, test := range []struct {
		name   string
		mutate func(*Result)
	}{
		{"foreign stage", func(r *Result) { r.Stage = stageCommit }},
		{"foreign outcome", func(r *Result) { r.Outcome = "failed" }},
		{"foreign code", func(r *Result) { r.Code = "import_done" }},
		{"arbitrary message", func(r *Result) { r.Message = "token=private-fixture" }},
		{"empty message", func(r *Result) { r.Message = "" }},
		{"invalid attempt time", func(r *Result) { r.AttemptedAt = "x" }},
		{"invalid success time", func(r *Result) { missing := "yesterday"; r.LastSuccessAt = &missing }},
		{"status on a parse result", func(r *Result) { r.Status = statusPointer(403) }},
		{"http result without status", func(r *Result) {
			r.Stage, r.Code, r.Outcome = stageHTTP, "source_http_status", "failed"
			r.Message, r.Status = resultMessage(r.Code, nil), nil
		}},
		{"http status out of range", func(r *Result) {
			bad := 999
			r.Stage, r.Code, r.Outcome, r.Status = stageHTTP, "source_http_status", "failed", &bad
			r.Message = resultMessage(r.Code, r.Status)
		}},
		{"committed cache disagrees", func(r *Result) { r.Counts.Cached = pointer(9) }},
		{"unknown protocol token", func(r *Result) { r.Diagnostics[0].Protocol = "not a protocol" }},
		{"foreign diagnostic message", func(r *Result) { r.Diagnostics[0].Message = "token=private-fixture" }},
		{"foreign diagnostic stage", func(r *Result) { r.Diagnostics[0].Stage = diagDuplicate }},
		{"foreign diagnostic field", func(r *Result) { r.Diagnostics[0].Field = "https://example.com/secret" }},
		{"entry index past the total", func(r *Result) { end := *r.Counts.Total + 3; r.Diagnostics[0].EntryIndex = &end }},
		{"kept index equal to the entry", func(r *Result) {
			r.Diagnostics[0].Code, r.Diagnostics[0].Stage = "duplicate_endpoint_key", diagDuplicate
			r.Diagnostics[0].Message, _ = diagnosticMessage(r.Diagnostics[0].Code)
			r.Diagnostics[0].KeptIndex = r.Diagnostics[0].EntryIndex
		}},
		{"duplicate without a kept entry", func(r *Result) {
			r.Diagnostics[0].Code, r.Diagnostics[0].Stage = "duplicate_endpoint_key", diagDuplicate
			r.Diagnostics[0].Message, _ = diagnosticMessage(r.Diagnostics[0].Code)
			r.Diagnostics[0].KeptIndex = nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := valid()
			test.mutate(&result)
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if ValidateResult(raw) == nil {
				t.Fatalf("invalid result accepted: %s", raw)
			}
		})
	}
	// A failed attempt retains nothing new and cannot claim usable endpoints.
	failed := failureResult(stamp, "source", committedSource{known: true, cached: 4}, stageDeadline, "source_timeout")
	failed.Counts.Usable = pointer(2)
	if raw, _ := json.Marshal(failed); ValidateResult(raw) == nil {
		t.Fatal("failed attempt with usable endpoints accepted")
	}
	failed.Counts.Usable, failed.Counts.Accepted = pointer(0), 1
	if raw, _ := json.Marshal(failed); ValidateResult(raw) == nil {
		t.Fatal("failed attempt with accepted endpoints accepted")
	}
	for _, broken := range []string{
		`{"outcome":"ok","stage":"parse","code":"import_ok","message":"已导入全部可识别节点","attemptedAt":"2026-11-05T00:00:00Z","counts":{"total":1,"accepted":1,"rejected":0,"duplicates":0,"cached":1,"usable":1},"diagnostics":[],"diagnosticsTruncated":false,"note":"token=private-fixture"}`,
		`{"outcome":"done","stage":"parse","code":"import_ok","attemptedAt":"2026-11-05T00:00:00Z","counts":{"accepted":0}}`,
		`{"outcome":"ok","stage":"parse","code":"import_ok","message":"已导入全部可识别节点","attemptedAt":"2026-11-05T00:00:00Z","counts":{"total":2,"accepted":2,"rejected":1,"duplicates":0,"cached":2,"usable":2},"diagnostics":[],"diagnosticsTruncated":false}`,
		`{"outcome":"ok","stage":"parse","code":"import_ok","message":"已导入全部可识别节点","counts":{"accepted":0},"diagnostics":[]}`,
		`{"outcome":"ok","stage":"parse","code":"import_ok","message":"已导入全部可识别节点","attemptedAt":"2026-11-05T00:00:00Z","counts":{"total":1,"accepted":1,"rejected":0,"duplicates":0,"cached":1,"usable":1},"diagnostics":[],"diagnosticsTruncated":false} {}`,
		`not json`,
	} {
		if ValidateResult([]byte(broken)) == nil {
			t.Fatalf("invalid result accepted: %s", broken)
		}
	}
}

// Exactly one JSON value: trailing rubbish is refused whether it is a second value
// or a syntax error, and trailing whitespace is not rubbish.
func TestValidateResultRefusesTrailingContent(t *testing.T) {
	stamp := "2026-11-05T00:00:00Z"
	committed, _ := Analyze([]byte("socks5://fixture:fixture@a.example.com:1080#A"))
	committed.SourceID, committed.AttemptedAt, committed.LastSuccessAt = "source", stamp, &stamp
	committed.Counts.Cached, committed.Counts.Usable = pointer(1), pointer(1)
	raw, err := json.Marshal(committed)
	if err != nil {
		t.Fatal(err)
	}
	for _, trailing := range []string{" !garbage", " x", "{}", `{"outcome":"ok"}`, "\n0", ` null`} {
		if ValidateResult(append(append([]byte{}, raw...), trailing...)) == nil {
			t.Fatalf("trailing %q accepted", trailing)
		}
	}
	for _, trailing := range []string{"", "\n", " \n\t "} {
		if err = ValidateResult(append(append([]byte{}, raw...), trailing...)); err != nil {
			t.Fatalf("trailing %q rejected: %v", trailing, err)
		}
	}
}

// A result can only be stored when the store writes it, when its committed success is
// its own attempt, and when an outcome agrees with its counts. Each negative below is
// a state the panel never produces, so an archive carrying one is not its own.
func TestValidateResultRefusesUnreachableStates(t *testing.T) {
	stamp := "2026-11-05T00:00:00Z"
	earlier := "2026-11-04T00:00:00Z"
	committed := func() Result {
		result, _ := Analyze([]byte("socks5://fixture:fixture@a.example.com:1080#A"))
		result.SourceID, result.AttemptedAt, result.LastSuccessAt = "source", stamp, &stamp
		result.Counts.Cached, result.Counts.Usable = pointer(1), pointer(1)
		return result
	}
	raw := func(t *testing.T, result Result) []byte {
		t.Helper()
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	// A committed result with a changed stamp is not this panel's.
	for _, test := range []struct {
		name string
		// storedOnly marks a state the shared verifier accepts but the store never
		// writes, so only the row comparison can refuse it.
		storedOnly bool
		mutate     func(*Result)
	}{
		{"success without a success time", false, func(r *Result) { r.LastSuccessAt = nil }},
		{"success time from another attempt", false, func(r *Result) { r.LastSuccessAt = &earlier }},
		{"ok outcome with rejected entries", false, func(r *Result) { changeCounts(r, 1, 0) }},
		{"ok outcome with duplicates", false, func(r *Result) { changeCounts(r, 0, 1) }},
		{"partial outcome without skips", false, func(r *Result) {
			r.Outcome, r.Code, r.Message = "partial", "import_partial", resultMessage("import_partial", nil)
		}},
		{"stored storage failure", true, func(r *Result) {
			failure, _ := commitFailure("source", stamp, committedSource{known: true, cached: 1, lastSuccess: &earlier}, errFixture)
			*r = failure
		}},
		{"stored refused paste", true, func(r *Result) {
			*r = rejectedPaste(stamp, "source", committedSource{}, mustAnalyze(t, "vmess://unsupported.example.com:443"))
		}},
		{"stored failure without its cache", true, func(r *Result) {
			*r = failureResult(stamp, "source", committedSource{known: true, cached: 1, lastSuccess: &earlier}, stageDNS, "source_dns_failed")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := committed()
			test.mutate(&result)
			data := raw(t, result)
			if ValidateResult(data) != nil {
				if test.storedOnly {
					t.Fatalf("the shared verifier already refuses this: %s", data)
				}
				return
			}
			if !test.storedOnly {
				t.Fatalf("the shared verifier accepted an unreachable state: %s", data)
			}
			if ValidateStoredResult("source", stamp, stamp, "ok", data) == nil {
				t.Fatalf("a state the store never writes was persisted: %s", data)
			}
		})
	}
	// The own results of the panel are accepted, so the negatives above are about the
	// state, not about the verifier being unreachable.
	for _, result := range []Result{
		committed(),
		rejectedPaste(stamp, "source", committedSource{}, mustAnalyze(t, "vmess://unsupported.example.com:443")),
		rejectedPaste(stamp, "source", committedSource{known: true, cached: 1, lastSuccess: &earlier, status: "ok"}, mustAnalyze(t, "vmess://unsupported.example.com:443")),
	} {
		if err := ValidateResult(raw(t, result)); err != nil {
			t.Fatalf("own result rejected: %+v %v", result, err)
		}
	}
}

// changeCounts makes an ok result contradict its own counts, keeping total consistent.
func changeCounts(result *Result, rejected, duplicates int) {
	result.Outcome, result.Code, result.Message = "ok", "import_ok", resultMessage("import_ok", nil)
	total := result.Counts.Accepted + rejected + duplicates
	result.Counts.Total, result.Counts.Rejected, result.Counts.Duplicates = &total, &rejected, &duplicates
}

// errFixture stands in for the storage error a commit failure carries.
var errFixture = errors.New("fixture storage failure")

func mustAnalyze(t *testing.T, content string) Result {
	t.Helper()
	result, _ := Analyze([]byte(content))
	return result
}

// never replaced by zero.
func TestResultMessagesAreFixed(t *testing.T) {
	for code := range resultSpecs {
		if code == "source_http_status" {
			if text := resultMessage(code, statusPointer(403)); !strings.Contains(text, "HTTP 403") {
				t.Fatalf("status missing: %q", text)
			}
			for _, status := range []*int{nil, statusPointer(0), statusPointer(99), statusPointer(600)} {
				if text := resultMessage(code, status); strings.Contains(text, "0") {
					t.Fatalf("status %v stated as zero: %q", status, text)
				}
			}
			continue
		}
		first, second := resultMessage(code, nil), resultMessage(code, nil)
		if first == "" || first != second {
			t.Fatalf("message for %q: %q", code, first)
		}
	}
	if resultMessage("unknown_code", nil) != "" {
		t.Fatal("an unknown code has text")
	}
	if text := (&FetchError{Stage: stageHTTP, Code: "source_http_status", Status: 403}).Message(); !strings.Contains(text, "HTTP 403") {
		t.Fatalf("fetch message %q", text)
	}
	if text := (&FetchError{Code: "source_http_status"}).Message(); strings.Contains(text, "HTTP 0") {
		t.Fatalf("fetch message states zero: %q", text)
	}
}

// A stored result must belong to its own row: same source, same attempt and success
// time, and the status the result implies. A pending source has no result at all.
func TestValidateStoredResultMatchesRow(t *testing.T) {
	stamp := "2026-11-05T00:00:00Z"
	earlier := "2026-11-04T00:00:00Z"
	committed, _ := Analyze([]byte("socks5://fixture:fixture@a.example.com:1080#A"))
	committed.SourceID, committed.AttemptedAt, committed.LastSuccessAt = "source", stamp, &stamp
	committed.Counts.Cached, committed.Counts.Usable = pointer(1), pointer(1)
	raw, err := json.Marshal(committed)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateStoredResult("source", stamp, stamp, "ok", raw); err != nil {
		t.Fatalf("own row rejected: %v", err)
	}
	for _, test := range []struct {
		name        string
		sourceID    string
		lastFetch   string
		lastSuccess string
		status      string
	}{
		{"foreign source", "other", stamp, stamp, "ok"},
		{"empty source", "", stamp, stamp, "ok"},
		{"foreign attempt time", "source", earlier, stamp, "ok"},
		{"missing attempt time", "source", "", stamp, "ok"},
		{"missing success time", "source", stamp, "", "ok"},
		{"foreign success time", "source", stamp, earlier, "ok"},
		{"pending source with a result", "source", stamp, stamp, "pending"},
		{"failed row with a successful result", "source", stamp, stamp, "failed"},
	} {
		if ValidateStoredResult(test.sourceID, test.lastFetch, test.lastSuccess, test.status, raw) == nil {
			t.Fatalf("%s accepted", test.name)
		}
	}
	failed := failureResult(stamp, "source", committedSource{known: true, cached: 1, lastSuccess: &earlier}, stageDNS, "source_dns_failed")
	// A recorded failure always knows the cache it kept and that it is unusable.
	failed.Counts.Cached, failed.Counts.Usable = pointer(1), pointer(0)
	raw, err = json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateStoredResult("source", stamp, earlier, "failed", raw); err != nil {
		t.Fatalf("failed row rejected: %v", err)
	}
	if ValidateStoredResult("source", stamp, earlier, "ok", raw) == nil {
		t.Fatal("a failed result was accepted for an ok source")
	}
}
