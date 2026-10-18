package singbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"time"
)

// clashConnectionTotals is the part of the upstream Clash API `GET /connections`
// response the Agent reads. Official sing-box builds track these two counters
// cumulatively for the running process; they name no user (plan D5).
type clashConnectionTotals struct {
	UploadTotal   uint64 `json:"uploadTotal"`
	DownloadTotal uint64 `json:"downloadTotal"`
}

// nodeTotals keeps the previous Clash API sample so every read can report the
// bytes observed since the last one.
type nodeTotals struct {
	upload, download uint64
	sampled          bool
}

// delta returns the bytes observed since the previous sample. The Clash API
// totals are cumulative for one core process, so a total below the previous one
// means the core restarted: that direction's baseline restarts at zero and the
// new process's own bytes are counted, never as a negative and never twice.
func (t *nodeTotals) delta(current clashConnectionTotals) (uint64, uint64) {
	if !t.sampled {
		t.sampled = true
		t.upload, t.download = current.UploadTotal, current.DownloadTotal
		return current.UploadTotal, current.DownloadTotal
	}
	upload, download := current.UploadTotal, current.DownloadTotal
	if current.UploadTotal >= t.upload {
		upload = current.UploadTotal - t.upload
	}
	if current.DownloadTotal >= t.download {
		download = current.DownloadTotal - t.download
	}
	t.upload, t.download = current.UploadTotal, current.DownloadTotal
	return upload, download
}

// NodeTrafficTotals reports the node's cumulative upload and download bytes,
// diffed from the upstream Clash API so a core restart restarts the baseline
// instead of producing a negative or a double count. Official sing-box builds
// cannot attribute traffic to a user, so these node totals are all the core can
// report (plan D5): per-user traffic and online counts stay unknown.
//
// A stopped core, a missing Clash API secret or an unreachable API reports an
// error, never a zero.
func (a *Adapter) NodeTrafficTotals(ctx context.Context) (uint64, uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process == nil || a.current.StatsEndpoint == "" || a.statsSecret == "" {
		return 0, 0, errors.New("sing-box node traffic totals unavailable")
	}
	host, _, err := net.SplitHostPort(a.current.StatsEndpoint)
	if err != nil || host != "127.0.0.1" {
		return 0, 0, errors.New("node traffic totals require loopback")
	}
	current, err := readClashTotals(ctx, a.current.StatsEndpoint, a.statsSecret)
	if err != nil {
		return 0, 0, err
	}
	upload, download := a.nodeTotals.delta(current)
	if math.MaxUint64-a.nodeUpload < upload || math.MaxUint64-a.nodeDownload < download {
		return 0, 0, errors.New("node traffic total overflow")
	}
	a.nodeUpload += upload
	a.nodeDownload += download
	return a.nodeUpload, a.nodeDownload, nil
}

// readClashTotals reads the cumulative node counters from the loopback-only Clash
// API with the per-configuration secret. The Agent never renders a Clash API
// without a secret, so an unauthenticated request is a programming error rather
// than a supported mode.
func readClashTotals(ctx context.Context, endpoint, secret string) (clashConnectionTotals, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+endpoint+"/connections", nil)
	if err != nil {
		return clashConnectionTotals{}, err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return clashConnectionTotals{}, ctx.Err()
		}
		return clashConnectionTotals{}, errors.New("sing-box Clash API unreachable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return clashConnectionTotals{}, errors.New("sing-box Clash API rejected the node total request")
	}
	var totals clashConnectionTotals
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&totals); err != nil {
		return clashConnectionTotals{}, errors.New("invalid sing-box Clash API node totals")
	}
	return totals, nil
}
