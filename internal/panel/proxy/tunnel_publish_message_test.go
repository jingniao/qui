package proxy

import "testing"

func TestTunnelPublishFailureMessage(t *testing.T) {
	for _, tc := range []struct {
		name string
		node CanvasPublishNode
		want string
	}{
		{"automatic", CanvasPublishNode{Status: "rolled_back", AutomaticRollback: true, FailureCode: "apply_failed"}, "配置应用失败，已自动回滚（apply_failed）"},
		{"reverse", CanvasPublishNode{Status: "rolled_back"}, "后续服务器失败，已回滚"},
		{"failed", CanvasPublishNode{Status: "failed", FailureCode: "validation_failed"}, "服务器配置发布失败（validation_failed）"},
		{"no code", CanvasPublishNode{Status: "rolled_back", AutomaticRollback: true}, "配置应用失败，已自动回滚"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tunnelPublishFailureMessage(tc.node); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
