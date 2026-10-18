package traffic

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestTrafficBatchCumulativeMetadataStrictlyValidated(t *testing.T) {
	counter := Counter{CoreEpoch: "epoch", Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "vless", UploadBytes: 10, DownloadBytes: 20, ObservedAt: time.Now().UTC()}
	batch := Batch{ID: "batch", NodeID: "node", Counters: []Counter{counter}}
	data, _ := json.Marshal(batch)
	decoded, err := Decode(data)
	if err != nil || len(decoded.Counters) != 1 {
		t.Fatal(err)
	}
	for _, change := range []func(*Counter){func(c *Counter) { c.UploadBytes = -1 }, func(c *Counter) { c.Protocol = "shell" }, func(c *Counter) { c.CoreEpoch = "http://private/path" }, func(c *Counter) { c.ObservedAt = time.Time{} }} {
		bad := counter
		change(&bad)
		value := Batch{ID: "bad", NodeID: "node", Counters: []Counter{bad}}
		if err := value.Validate(); err == nil {
			t.Fatal("invalid traffic metadata accepted")
		}
	}
	batch.Counters = append(batch.Counters, counter)
	if err := batch.Validate(); err == nil {
		t.Fatal("duplicate source counter accepted")
	}
	if _, err := Decode([]byte(`{"id":"batch","node_id":"node","password":"private","counters":[]}`)); err == nil {
		t.Fatal("untyped content accepted")
	}
}

func TestNativeSampleAndWireBatchHaveSeparateBounds(t *testing.T) {
	values := make([]Counter, MaxSampleCounters+1)
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.FixedZone("fixture", 8*3600))
	for index := range values {
		values[index] = Counter{CoreEpoch: "epoch", Revision: 1, CredentialID: "credential-" + strconv.Itoa(index), UserID: "user", DeviceID: "device", Protocol: "http", UploadBytes: 1, ObservedAt: at}
	}
	if err := ValidateSample("node", values[:MaxSampleCounters]); err != nil {
		t.Fatal("bounded native sample rejected", err)
	}
	if !values[MaxBatchCounters].ObservedAt.Equal(at) || values[MaxBatchCounters].ObservedAt.Location() != time.UTC {
		t.Fatal("counter after wire boundary was not normalized to UTC")
	}
	if err := ValidateSample("node", values); err == nil {
		t.Fatal("unbounded native sample accepted")
	}
	if err := (&Batch{ID: "batch", NodeID: "node", Counters: values[:MaxBatchCounters]}).Validate(); err != nil {
		t.Fatal("bounded wire batch rejected", err)
	}
	if err := (&Batch{ID: "batch", NodeID: "node", Counters: values[:MaxBatchCounters+1]}).Validate(); err == nil {
		t.Fatal("large native sample expanded wire protocol limit")
	}
	for _, node := range []string{"", "../node", "https://node.example/path"} {
		if err := ValidateSample(node, values[:300]); err == nil {
			t.Fatal("invalid sample source accepted", node)
		}
	}
	if err := ValidateSample("node", nil); err == nil {
		t.Fatal("empty native sample accepted")
	}
	values[299] = values[0]
	if err := ValidateSample("node", values[:300]); err == nil {
		t.Fatal("duplicate across wire boundary accepted")
	}
	values[299].CredentialID = "invalid-counter"
	values[299].Protocol = "shell"
	if err := ValidateSample("node", values[:300]); err == nil {
		t.Fatal("invalid late counter escaped validation")
	}
}
