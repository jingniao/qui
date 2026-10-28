package node

import "testing"

type storedRow []any

func (r storedRow) Scan(dest ...any) error {
	for i, value := range r {
		switch target := dest[i].(type) {
		case *string:
			*target = value.(string)
		case *bool:
			*target = value.(bool)
		case *int64:
			*target = value.(int64)
		}
	}
	return nil
}

// A record written by a newer panel (a field this build does not know) must
// still scan, or a downgrade would hide every node until each Agent reconnects.
func TestScanIgnoresFieldsFromNewerPanels(t *testing.T) {
	row := storedRow{"n1", "node", "offline", true, "", "", "", "", "", "", "", "", "", int64(0),
		`{"coreName":"sing-box","fieldFromTheFuture":true}`, `{"cpuPercent":1,"anotherNewField":"x"}`,
		"", true, false, ""}
	n, err := scan(row)
	if err != nil {
		t.Fatal(err)
	}
	if n.Capabilities.GetCoreName() != "sing-box" || n.Heartbeat.GetCpuPercent() != 1 {
		t.Fatalf("known fields lost: %v %v", n.Capabilities, n.Heartbeat)
	}
}
