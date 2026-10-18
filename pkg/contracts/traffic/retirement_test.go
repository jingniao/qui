package traffic

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRetirementContractRejectsUnknownDataAndWrongBounds(t *testing.T) {
	valid := BudgetRetirement{ID: "request", NodeID: "node", UserID: "user", BudgetID: "grant", Sequence: 1, CeilingBytes: 96, SpentBytes: 48, PauseRevision: 2, StoreID: "store", SourceSequence: 2, CoreEpoch: "epoch", SourceDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	data, _ := json.Marshal(valid)
	if _, err := DecodeBudgetRetirement(data); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BudgetRetirement){func(r *BudgetRetirement) { r.ID = "../request" }, func(r *BudgetRetirement) { r.SpentBytes = 97 }, func(r *BudgetRetirement) { r.SpentBytes = -1 }, func(r *BudgetRetirement) { r.Sequence = 0 }, func(r *BudgetRetirement) { r.SourceDigest = "invalid" }, func(r *BudgetRetirement) { r.StoreID = "" }} {
		bad := valid
		mutate(&bad)
		body, _ := json.Marshal(bad)
		if _, err := DecodeBudgetRetirement(body); err == nil {
			t.Fatal("invalid retirement accepted", bad)
		}
	}
	for _, invalid := range [][]byte{append(data, []byte(" {}")...), bytes.Repeat([]byte("x"), MaxBudgetRetirementBytes+1)} {
		if _, err := DecodeBudgetRetirement(invalid); err == nil {
			t.Fatal("unbounded or multiple retirement objects accepted")
		}
	}
	receipt := BudgetRetirementReceipt{Request: valid, ClosedDigest: valid.SourceDigest}
	body, _ := json.Marshal(receipt)
	if _, err := DecodeBudgetRetirementReceipt(body); err != nil {
		t.Fatal(err)
	}
}
