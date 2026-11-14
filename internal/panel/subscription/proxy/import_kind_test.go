package proxy

import (
	"encoding/json"
	"testing"
)

func TestOriginImportKindIsRuntimeOnly(t *testing.T) {
	const original = `{"Relayed":false,"Source":"import:source","Ref":"node","Node":{"ID":"","Name":""},"Labels":null}`
	for _, kind := range []string{"", "content", "url"} {
		origin := Origin{Source: "import:source", Ref: "node", ImportKind: kind}
		raw, err := json.Marshal(origin)
		if err != nil || string(raw) != original {
			t.Fatalf("ImportKind %q changed JSON bytes: %s, %v", kind, raw, err)
		}
	}
	var origin Origin
	if err := json.Unmarshal([]byte(`{"Source":"import:source","ImportKind":"content"}`), &origin); err != nil {
		t.Fatal(err)
	}
	if origin.ImportKind != "" {
		t.Fatal("cached JSON supplied runtime import kind")
	}
}
