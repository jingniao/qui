// Hand-written companion of xray_stats.pb.go.
//
// The package is a hand-maintained, wire-compatible read-only subset of the
// upstream Xray StatsService proto at
// github.com/XTLS/Xray-core/app/stats/command/command.proto (package
// xray.app.stats.command). It carries QueryStats and the online RPC the Agent
// reads per-user counts from; the upstream service also defines GetStats,
// GetSysStats, GetStatsOnlineIpList, GetAllOnlineUsers and GetUsersStats, which
// the Agent never calls and which are therefore not part of this subset.
//
// protoc is not available in this environment, so the generated subset in this
// package cannot be regenerated from upstream. The online RPC is therefore
// invoked by name with the generated messages, which are wire-compatible with
// the upstream xray.app.stats.command pair:
//
//   - upstream GetStatsRequest {string name = 1; bool reset = 2;} has the same
//     field numbers and wire types as QueryStatsRequest {pattern = 1; reset = 2;};
//   - upstream GetStatsResponse {Stat stat = 1;} carries exactly one Stat
//     message, which QueryStatsResponse {repeated Stat stat = 1;} decodes into a
//     single-element slice.
//
// When protoc becomes available this package can be replaced by the full
// upstream generated code; no caller depends on the extra upstream messages.

package xraystatspb

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StatsService_GetStatsOnline_FullMethodName is the upstream method that returns
// the online count of one online map.
const StatsService_GetStatsOnline_FullMethodName = "/xray.app.stats.command.StatsService/GetStatsOnline"

// GetStatsOnline reads the online count of the named online map. Xray registers
// one map per user, named "user>>>" + email + ">>>online", and counts the
// distinct client IPs currently tracked for that user (localhost excluded); the
// count is never reset. A user whose map was never registered has no tracked
// client IP yet, so an upstream NotFound answers zero instead of an error.
func GetStatsOnline(ctx context.Context, cc grpc.ClientConnInterface, name string, opts ...grpc.CallOption) (int64, error) {
	if name == "" {
		return 0, errors.New("online statistics require a name")
	}
	response := &QueryStatsResponse{}
	err := cc.Invoke(ctx, StatsService_GetStatsOnline_FullMethodName, &QueryStatsRequest{Pattern: name, Reset_: false}, response, opts...)
	if status.Code(err) == codes.NotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if len(response.Stat) != 1 || response.Stat[0] == nil || response.Stat[0].Name != name || response.Stat[0].Value < 0 {
		return 0, errors.New("invalid online statistic")
	}
	return response.Stat[0].Value, nil
}
