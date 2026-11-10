package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node/tuningadvice"
	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/tuning"
)

type AdviceSources interface {
	HasQUICInbound(ctx context.Context, nodeID string) (bool, error)
	RelayRole(ctx context.Context, nodeID string) (bool, error)
	MedianRTT(ctx context.Context, nodeID string) (*float64, error)
}

type dummyAdviceSources struct{}

func (dummyAdviceSources) HasQUICInbound(context.Context, string) (bool, error) { return false, nil }
func (dummyAdviceSources) RelayRole(context.Context, string) (bool, error)      { return false, nil }
func (dummyAdviceSources) MedianRTT(context.Context, string) (*float64, error)  { return nil, nil }

func (r *Repository) adviceSources() AdviceSources {
	if r.AdviceSources != nil {
		return r.AdviceSources
	}
	return dummyAdviceSources{}
}

type RecommendationItem struct {
	tuningadvice.Advice
	Dismissed bool `json:"dismissed"`
}

type RecommendationView struct {
	RulesVersion string               `json:"rulesVersion"`
	Items        []RecommendationItem `json:"items"`
}

type TuningAutoApply struct {
	Enabled  bool  `json:"enabled"`
	Revision int64 `json:"revision"`
}

type dismissalKey struct {
	key   string
	value string
}

func (r *Repository) nodeRecommendations(ctx context.Context, n Node) RecommendationView {
	view := RecommendationView{
		RulesVersion: tuningadvice.RulesVersion,
		Items:        []RecommendationItem{},
	}
	if n.Capabilities == nil || !n.Capabilities.GetTuningSupported() || n.Capabilities.Host == nil {
		return view
	}

	// 1. Read dismissals for this node
	dismissals := map[dismissalKey]bool{}
	rows, err := r.Database.QueryContext(ctx, "SELECT key, value, rules_version FROM node_tuning_dismissals WHERE node_id=?", n.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k, v, ver string
			if err := rows.Scan(&k, &v, &ver); err == nil && ver == tuningadvice.RulesVersion {
				dismissals[dismissalKey{key: k, value: v}] = true
			}
		}
	}

	// 2. Read bandwidth directly from node_meta (avoid r.Meta calling r.Get recursion)
	var bandwidth *int
	_ = r.Database.QueryRowContext(ctx, "SELECT bandwidth_mbps FROM node_meta WHERE node_id=?", n.ID).Scan(&bandwidth)

	// 3. Query AdviceSources
	hasQUIC, _ := r.adviceSources().HasQUICInbound(ctx, n.ID)
	relay, _ := r.adviceSources().RelayRole(ctx, n.ID)
	medianRTT, _ := r.adviceSources().MedianRTT(ctx, n.ID)

	// 4. Build Inputs
	host := n.Capabilities.Host
	keys := make(map[string]tuningadvice.KeyState, len(host.TuningKeys))
	for _, k := range host.TuningKeys {
		if k != nil {
			keys[k.Key] = tuningadvice.KeyState{Present: k.Present, Writable: k.Writable}
		}
	}

	sources := make(map[string]string, len(host.SysctlSources))
	for _, s := range host.SysctlSources {
		if s != nil {
			sources[s.Key] = s.File
		}
	}

	current := n.Heartbeat.GetTuningCurrent()
	if current == nil {
		current = map[string]string{}
	}

	inputs := tuningadvice.Inputs{
		MemoryBytes:         host.MemoryTotalBytes,
		CPUCores:            int(host.CpuCores),
		Virtualization:      host.Virtualization,
		Keys:                keys,
		Current:             current,
		Sources:             sources,
		CongestionAvailable: host.CongestionAvailable,
		CongestionLoadable:  host.CongestionLoadable,
		QdiscAvailable:      host.QdiscAvailable,
		QdiscLoadable:       host.QdiscLoadable,
		HasQUICInbound:      hasQUIC,
		RelayRole:           relay,
		MedianRTTMillis:     medianRTT,
		BandwidthMbps:       bandwidth,
	}

	advices := tuningadvice.Recommend(inputs)
	for _, adv := range advices {
		view.Items = append(view.Items, RecommendationItem{
			Advice:    adv,
			Dismissed: dismissals[dismissalKey{key: adv.Key, value: adv.Value}],
		})
	}
	return view
}

func (r *Repository) decorateTuningAdvice(ctx context.Context, nodes []Node) error {
	for i := range nodes {
		if nodes[i].Capabilities == nil || !nodes[i].Capabilities.GetTuningSupported() || nodes[i].Capabilities.Host == nil {
			nodes[i].TuningAdviceCount = 0
			continue
		}
		view := r.nodeRecommendations(ctx, nodes[i])
		var count int
		for _, item := range view.Items {
			if item.Level == "suggested" && !item.Dismissed {
				count++
			}
		}
		nodes[i].TuningAdviceCount = count
	}
	return nil
}

func (h *Handler) tuningRecommendations(w http.ResponseWriter, r *http.Request) {
	n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.DeletedAt != "" {
		h.fail(w, ErrNotFound)
		return
	}
	view := h.Repository.nodeRecommendations(r.Context(), n)
	httpapi.JSON(w, http.StatusOK, view)
}

func (h *Handler) dismissTuningRecommendation(w http.ResponseWriter, r *http.Request) {
	n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.DeletedAt != "" {
		h.fail(w, ErrNotFound)
		return
	}

	var input struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if input.Key == "" || input.Value == "" {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", "key and value required")
		return
	}
	if !tuning.Known(input.Key) {
		httpapi.Error(w, http.StatusBadRequest, "invalid_key", "unknown tuning key")
		return
	}

	now := timestamp(time.Now())
	_, err = h.Repository.Database.ExecContext(r.Context(), `INSERT INTO node_tuning_dismissals(node_id,key,value,rules_version,dismissed_at)
VALUES(?,?,?,?,?)
ON CONFLICT(node_id,key) DO UPDATE SET value=excluded.value,rules_version=excluded.rules_version,dismissed_at=excluded.dismissed_at`,
		n.ID, input.Key, input.Value, tuningadvice.RulesVersion, now)
	if err != nil {
		h.fail(w, err)
		return
	}

	if h.Audit != nil {
		_ = h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "不采纳调优建议", n.Name+" ("+n.ID+")", "成功", r.RemoteAddr)
	}

	httpapi.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) tuningAutoApply(w http.ResponseWriter, r *http.Request) {
	setting, err := h.Repository.ReadTuningAutoApply(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, setting)
}

func (h *Handler) putTuningAutoApply(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled          bool  `json:"enabled"`
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	actor := auth.Actor(r.Context()).Username
	setting, err := h.Repository.SaveTuningAutoApply(r.Context(), input.Enabled, input.ExpectedRevision, actor, r.RemoteAddr, h.Audit)
	if err != nil {
		if errors.Is(err, errConflict) {
			httpapi.Error(w, http.StatusConflict, "conflict", "revision mismatch")
			return
		}
		h.fail(w, err)
		return
	}

	httpapi.JSON(w, http.StatusOK, setting)
}

var errConflict = errors.New("revision conflict")

func (r *Repository) ReadTuningAutoApply(ctx context.Context) (TuningAutoApply, error) {
	var encoded string
	err := r.Database.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='tuning_auto_apply'").Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return TuningAutoApply{Enabled: false, Revision: 0}, nil
	}
	if err != nil {
		return TuningAutoApply{}, err
	}
	var setting TuningAutoApply
	if err := json.Unmarshal([]byte(encoded), &setting); err != nil {
		return TuningAutoApply{Enabled: false, Revision: 0}, nil
	}
	return setting, nil
}

func (r *Repository) SaveTuningAutoApply(ctx context.Context, enabled bool, expectedRevision int64, actor, ip string, audit auditLog) (TuningAutoApply, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return TuningAutoApply{}, err
	}
	defer tx.Rollback()

	var encoded string
	current := TuningAutoApply{Enabled: false, Revision: 0}
	err = tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='tuning_auto_apply'").Scan(&encoded)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TuningAutoApply{}, err
	}
	if err == nil {
		_ = json.Unmarshal([]byte(encoded), &current)
	}

	if current.Revision != expectedRevision {
		return TuningAutoApply{}, errConflict
	}

	next := TuningAutoApply{
		Enabled:  enabled,
		Revision: current.Revision + 1,
	}

	type storedSetting struct {
		Enabled   bool   `json:"enabled"`
		Revision  int64  `json:"revision"`
		UpdatedAt string `json:"updatedAt"`
		UpdatedBy string `json:"updatedBy"`
	}
	data, _ := json.Marshal(storedSetting{
		Enabled:   next.Enabled,
		Revision:  next.Revision,
		UpdatedAt: timestamp(time.Now()),
		UpdatedBy: actor,
	})

	_, err = tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('tuning_auto_apply',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", string(data))
	if err != nil {
		return TuningAutoApply{}, err
	}

	if audit != nil {
		if err := audit.RecordTx(ctx, tx, actor, "修改调优自动应用", "tuning_auto_apply", "成功", ip); err != nil {
			return TuningAutoApply{}, err
		}
	}

	return next, tx.Commit()
}

func (c *Control) maybeAutoApplyTuning(ctx context.Context, nodeID string, caps *pb.Capabilities, isFirstCapabilities bool) error {
	if !isFirstCapabilities || caps == nil || !caps.GetTuningSupported() || caps.Host == nil {
		return nil
	}

	setting, err := c.Repository.ReadTuningAutoApply(ctx)
	if err != nil || !setting.Enabled {
		return nil
	}

	var count int
	if err := c.Repository.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_tuning WHERE node_id=?", nodeID).Scan(&count); err != nil || count > 0 {
		return nil
	}

	n, err := c.Repository.Get(ctx, nodeID)
	if err != nil || n.Capabilities == nil || n.Capabilities.Host == nil {
		return nil
	}

	view := c.Repository.nodeRecommendations(ctx, n)
	settings := map[string]string{}
	for _, item := range view.Items {
		if item.Level == "suggested" && !item.Dismissed {
			settings[item.Key] = item.Value
		}
	}
	if len(settings) == 0 {
		return nil
	}

	if err := ValidateTuning(settings, false); err != nil {
		return err
	}

	requestID, err := RandomID()
	if err != nil {
		return err
	}

	raw, _ := json.Marshal(settings)
	now := timestamp(time.Now())

	tx, err := c.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Double-check no tuning record was inserted concurrently
	var existing int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM node_tuning WHERE node_id=?", nodeID).Scan(&existing); err != nil || existing > 0 {
		return nil
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO node_tuning(node_id,desired_json,request_id,status,requested_at,updated_at)
VALUES(?,?,?,'pending',?,?)`, nodeID, string(raw), requestID, now, now)
	if err != nil {
		return err
	}

	if c.Audit != nil {
		if err := c.Audit.RecordTx(ctx, tx, "auto", "自动应用调优推荐", n.Name+" ("+n.ID+")", "成功", ""); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if c.Notify != nil {
		c.Notify(nodeID)
	}

	return nil
}