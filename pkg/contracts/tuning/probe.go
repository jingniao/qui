package tuning

import (
	"fmt"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func HasModuleTarget(settings map[string]string) bool {
	return settings["net.ipv4.tcp_congestion_control"] == "bbr" || settings["net.core.default_qdisc"] == "fq"
}

// Empty status identifies legacy workers. Clearing a profile restores its
// baseline and is still available when a new probe is unavailable.
func CheckProbe(settings map[string]string, status string, keys []*pb.TuningKeyState) error {
	if len(settings) == 0 || status == "" {
		return nil
	}
	if status != "ready" {
		return fmt.Errorf("调优权限尚未就绪，请检查本机 Agent 与 helper：%s", status)
	}
	for key := range settings {
		var state *pb.TuningKeyState
		for _, candidate := range keys {
			if candidate.GetKey() == key {
				state = candidate
				break
			}
		}
		if state == nil {
			return fmt.Errorf("%s：缺少权限探测结果", key)
		}
		if !state.Present || !state.Writable {
			return fmt.Errorf("%s：不可写，%s", key, state.ProbeError)
		}
	}
	return nil
}
