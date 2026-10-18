package release

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestFixedUpdateEnvelopeRejectsPathsScriptsDuplicatesAndUnprovenResults(t *testing.T) {
	signed, _, _ := releaseFixture(t)
	signed.Manifest.Component = "xray"
	r := UpdateRequest{SchemaVersion: 1, ID: "upd_request", NodeID: "node-a", Revision: 8, Operation: "install", Release: signed}
	data, _ := json.Marshal(r)
	if _, err := DecodeUpdateRequest(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		bytes.Replace(data, []byte(`"schema_version":1`), []byte(`"schema_version":1,"shell":"touch /tmp/payload"`), 1),
		bytes.Replace(data, []byte(`"revision":8`), []byte(`"revision":8,"revision":9`), 1),
		append(append([]byte{}, data...), []byte(`{}`)...),
		bytes.Replace(data, []byte(`"operation":"install"`), []byte(`"operation":"script"`), 1),
	} {
		if _, err := DecodeUpdateRequest(bad); err == nil {
			t.Fatal("arbitrary or ambiguous command accepted")
		}
	}
	result := UpdateResult{ID: r.ID, NodeID: r.NodeID, Revision: r.Revision, TargetDigest: r.Release.Manifest.SHA256, Status: "success", Code: "success", InstalledDigest: r.Release.Manifest.SHA256, InstalledVersion: r.Release.Manifest.Version, InstalledSequence: 1}
	if result.Validate() != nil {
		t.Fatal("valid result rejected")
	}
	result.InstalledDigest = ""
	result.InstalledVersion = ""
	result.InstalledSequence = 0
	if result.Validate() == nil {
		t.Fatal("success without installed identity accepted")
	}
	result.Status = "failed_closed"
	result.Code = "installation_failed"
	if result.Validate() != nil {
		t.Fatal("closed uncertainty rejected")
	}
}
