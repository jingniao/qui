package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"
)

// MachineDayLayout is the canonical text of one local calendar day bucket.
const MachineDayLayout = "2006-01-02"

const (
	machineMethodSum      = "sum"
	machineMethodOutbound = "outbound"
	machineMethodMax      = "max"
	// The allowance stays inside JavaScript's exact integer range so the editor
	// needs no second string-number convention; the counters keep int64 range.
	machineAllowanceMax   = int64(1<<53 - 1)
	machineStatusUnknown  = "unknown"
	machineStatusStale    = "stale"
	machineStatusMeasured = "measured"
)

// ErrMachineTrafficOverflow refuses an aggregate SQLite would promote to REAL.
var ErrMachineTrafficOverflow = errors.New("machine traffic counter overflow")

// MachineTrafficSettings is the Owner's complete machine-traffic configuration
// of one server: the method that forms one number from both directions, the
// accounted interface (empty means automatic), the display-only allowance and
// the billing reset day.
type MachineTrafficSettings struct {
	Method         string `json:"method"`
	InterfaceName  string `json:"interfaceName"`
	AllowanceBytes *int64 `json:"allowanceBytes"`
	ResetDay       int    `json:"resetDay"`
}

func defaultMachineTrafficSettings() MachineTrafficSettings {
	return MachineTrafficSettings{Method: machineMethodSum, ResetDay: 1}
}

// ValidateMachineTrafficSettings is the one ruleset for the API and the backup
// checker. An interface name need not exist on the host: an offline server can
// be configured and reads as unknown until it is measured.
func ValidateMachineTrafficSettings(settings MachineTrafficSettings) error {
	if settings.Method != machineMethodSum && settings.Method != machineMethodOutbound && settings.Method != machineMethodMax {
		return errors.New("method must be sum, outbound or max")
	}
	if !ValidMachineTrafficInterface(settings.InterfaceName, true) {
		return errors.New("interfaceName is not a valid interface name")
	}
	if settings.AllowanceBytes != nil && (*settings.AllowanceBytes < 1 || *settings.AllowanceBytes > machineAllowanceMax) {
		return errors.New("allowanceBytes must be between 1 and 9007199254740991, or null")
	}
	if settings.ResetDay < 1 || settings.ResetDay > 31 {
		return errors.New("resetDay must be between 1 and 31")
	}
	return nil
}

// ValidMachineTrafficInterface accepts a Linux interface name for accounting:
// 1..15 bytes, no whitespace, control characters, "/" or ":", and never the
// loopback. Empty selects the automatic interface and is allowed only in
// settings.
func ValidMachineTrafficInterface(name string, allowEmpty bool) bool {
	if name == "" {
		return allowEmpty
	}
	if len(name) > 15 || name == "lo" || !utf8.ValidString(name) {
		return false
	}
	for _, char := range name {
		if unicode.IsSpace(char) || unicode.IsControl(char) || char == '/' || char == ':' {
			return false
		}
	}
	return true
}

// ValidBootID accepts the kernel's canonical UUID text only, so a value invented
// by an old or broken Agent is never used as a boot identity.
func ValidBootID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for _, char := range value {
		if char != '-' && !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

// ValidMachineDay accepts a canonical local date, exactly as stored.
func ValidMachineDay(day string) bool {
	parsed, err := time.Parse(MachineDayLayout, day)
	return err == nil && parsed.Format(MachineDayLayout) == day
}

// MachineTrafficSummary is one server's machine-wide usage of its current
// billing period. Byte totals are decimal strings so an int64 total never passes
// through a rounded JavaScript number.
type MachineTrafficSummary struct {
	NodeID         string                 `json:"nodeId"`
	Settings       MachineTrafficSettings `json:"settings"`
	InterfaceName  *string                `json:"interfaceName"`
	Timezone       string                 `json:"timezone"`
	PeriodStart    string                 `json:"periodStart"`
	PeriodEnd      string                 `json:"periodEnd"`
	NextResetAt    string                 `json:"nextResetAt"`
	RxBytes        *string                `json:"rxBytes"`
	TxBytes        *string                `json:"txBytes"`
	UsedBytes      *string                `json:"usedBytes"`
	Status         string                 `json:"status"`
	LastObservedAt *string                `json:"lastObservedAt"`
}

// localDayStart returns the first instant of a local calendar day. Where a DST
// change skips midnight (e.g. America/Santiago), time.Date normalizes 00:00 back
// into the previous day; the day then starts at the first hour that exists.
func localDayStart(year int, month time.Month, day int, loc *time.Location) time.Time {
	noon := time.Date(year, month, day, 12, 0, 0, 0, loc)
	start := time.Date(noon.Year(), noon.Month(), noon.Day(), 0, 0, 0, 0, loc)
	for start.Day() != noon.Day() {
		start = start.Add(time.Hour)
	}
	return start
}

// machineAnchor returns the start of the requested reset day in one month,
// clamped to that month's last day.
func machineAnchor(year int, month time.Month, day int, loc *time.Location) time.Time {
	last := time.Date(year, month+1, 0, 12, 0, 0, 0, loc).Day()
	return localDayStart(year, month, min(day, last), loc)
}

// machinePeriod returns the half-open billing period containing now and its
// exclusive end, which is also the next reset. Each anchor is built from the
// requested day, so January 31 is followed by February 28 and then March 31.
func machinePeriod(resetDay int, now time.Time, loc *time.Location) (time.Time, time.Time) {
	local := now.In(loc)
	start := machineAnchor(local.Year(), local.Month(), resetDay, loc)
	if local.Before(start) {
		start = machineAnchor(local.Year(), local.Month()-1, resetDay, loc)
	}
	return start, machineAnchor(start.Year(), start.Month()+1, resetDay, loc)
}

// machineDayShares splits one direction's delta over the local calendar days
// that overlap [from, to), in proportion to the elapsed time in each day. Every
// day but the last is floored and the last receives the exact remainder, so the
// shares always add up to the delta. Days are calendar midnights, so a 23- or
// 25-hour day works.
func machineDayShares(delta uint64, from, to time.Time, loc *time.Location) map[string]uint64 {
	if !from.Before(to) {
		return nil
	}
	total := to.Sub(from)
	shares := map[string]uint64{}
	remaining := delta
	for at := from; ; {
		local := at.In(loc)
		day := local.Format(MachineDayLayout)
		next := localDayStart(local.Year(), local.Month(), local.Day()+1, loc)
		if !next.Before(to) {
			shares[day] += remaining
			return shares
		}
		share := machineShare(delta, next.Sub(at), total)
		shares[day] += share
		remaining -= share
		at = next
	}
}

// machineShare is floor(delta*part/total) in exact integer arithmetic; a plain
// multiplication would overflow for a large delta.
func machineShare(delta uint64, part, total time.Duration) uint64 {
	numerator := new(big.Int).SetUint64(delta)
	numerator.Mul(numerator, big.NewInt(int64(part)))
	numerator.Div(numerator, big.NewInt(int64(total)))
	return numerator.Uint64()
}

// addMachineBytes adds a nonnegative delta to a nonnegative counter and refuses
// the int64 overflow SQLite would otherwise promote to REAL.
func addMachineBytes(current int64, delta uint64) (int64, bool) {
	if current < 0 || delta > math.MaxInt64 || current > math.MaxInt64-int64(delta) {
		return 0, false
	}
	return current + int64(delta), true
}

// machineUsed forms the period number from the two direction sums.
func machineUsed(method string, rx, tx int64) (string, error) {
	if method == machineMethodOutbound {
		return strconv.FormatInt(tx, 10), nil
	}
	if method == machineMethodMax {
		return strconv.FormatInt(max(rx, tx), 10), nil
	}
	sum, ok := addMachineBytes(rx, uint64(tx))
	if !ok {
		return "", ErrMachineTrafficOverflow
	}
	return strconv.FormatInt(sum, 10), nil
}

// timezoneTx reads the panel zone of this transaction. The reader is injected at
// composition so this package does not import system.
func (r *Repository) timezoneTx(ctx context.Context, tx *sql.Tx) (string, error) {
	if r.Timezone == nil {
		return "", errors.New("panel timezone reader is not configured")
	}
	return r.Timezone(ctx, tx)
}

// machineTrafficSettingsRows loads stored settings in one query. A node without
// a row keeps the defaults; reading never writes them.
func machineTrafficSettingsRows(ctx context.Context, q rowQuery, only string) (map[string]MachineTrafficSettings, error) {
	query, args := "SELECT node_id,method,interface_name,allowance_bytes,reset_day FROM node_machine_traffic_settings", []any{}
	if only != "" {
		query, args = query+" WHERE node_id=?", []any{only}
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string]MachineTrafficSettings{}
	for rows.Next() {
		var id string
		var settings MachineTrafficSettings
		if err := rows.Scan(&id, &settings.Method, &settings.InterfaceName, &settings.AllowanceBytes, &settings.ResetDay); err != nil {
			return nil, err
		}
		values[id] = settings
	}
	return values, rows.Err()
}

// MachineTrafficSettings returns one node's settings, or the defaults when it
// has never saved any.
func (r *Repository) MachineTrafficSettings(ctx context.Context, id string) (MachineTrafficSettings, error) {
	values, err := machineTrafficSettingsRows(ctx, r.Database, id)
	if err != nil {
		return MachineTrafficSettings{}, err
	}
	return machineSettingsFor(values, id), nil
}

// saveMachineTrafficSettingsTx replaces the complete settings of one node.
func saveMachineTrafficSettingsTx(ctx context.Context, tx *sql.Tx, id string, settings MachineTrafficSettings) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO node_machine_traffic_settings(node_id,method,interface_name,allowance_bytes,reset_day) VALUES(?,?,?,?,?)
ON CONFLICT(node_id) DO UPDATE SET method=excluded.method,interface_name=excluded.interface_name,allowance_bytes=excluded.allowance_bytes,reset_day=excluded.reset_day`, id, settings.Method, settings.InterfaceName, settings.AllowanceBytes, settings.ResetDay)
	return err
}

type machineReportedNode struct {
	heartbeat  *pb.Heartbeat
	reportedAt time.Time
	reported   bool
}

// machineTrafficNodes loads the stored heartbeat of one node or of every live
// node. A deleted node is absent, exactly like a missing one.
func machineTrafficNodes(ctx context.Context, q rowQuery, only string) (map[string]machineReportedNode, []string, error) {
	query, args := "SELECT id,heartbeat_json,last_heartbeat FROM nodes WHERE deleted_at IS NULL", []any{}
	if only != "" {
		query, args = query+" AND id=?", []any{only}
	}
	rows, err := q.QueryContext(ctx, query+" ORDER BY created_at,id", args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	nodes, order := map[string]machineReportedNode{}, []string{}
	for rows.Next() {
		var id, heartbeat, last string
		if err := rows.Scan(&id, &heartbeat, &last); err != nil {
			return nil, nil, err
		}
		node := machineReportedNode{heartbeat: &pb.Heartbeat{}}
		if err := storedJSON.Unmarshal([]byte(heartbeat), node.heartbeat); err != nil {
			return nil, nil, err
		}
		node.reportedAt, err = time.Parse(time.RFC3339Nano, last)
		node.reported = err == nil
		nodes[id], order = node, append(order, id)
	}
	return nodes, order, rows.Err()
}

type machineDailyRow struct {
	iface string
	day   string
	rx    int64
	tx    int64
}

// machineDailyRows reads retained daily rows of one node or of every live node
// inside the widest period any of them has.
func machineDailyRows(ctx context.Context, q rowQuery, only, from, to string) (map[string][]machineDailyRow, error) {
	query, args := "SELECT node_id,interface_name,local_date,rx_bytes,tx_bytes FROM node_machine_traffic_daily WHERE local_date>=? AND local_date<?", []any{from, to}
	if only != "" {
		query, args = query+" AND node_id=?", append(args, only)
	}
	rows, err := q.QueryContext(ctx, query+" ORDER BY node_id,local_date", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[string][]machineDailyRow{}
	for rows.Next() {
		var node string
		var row machineDailyRow
		if err := rows.Scan(&node, &row.iface, &row.day, &row.rx, &row.tx); err != nil {
			return nil, err
		}
		values[node] = append(values[node], row)
	}
	return values, rows.Err()
}

type machineCursorKey struct{ node, iface string }

// machineCursors returns the last observation time of every stored interface
// cursor. Cursors outlive an outage, so an offline server keeps reporting when
// its counters were last read.
func machineCursors(ctx context.Context, q rowQuery, only string) (map[machineCursorKey]string, error) {
	query, args := "SELECT node_id,interface_name,observed_at FROM node_machine_traffic_cursors", []any{}
	if only != "" {
		query, args = query+" WHERE node_id=?", []any{only}
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := map[machineCursorKey]string{}
	for rows.Next() {
		var node, iface, observed string
		if err := rows.Scan(&node, &iface, &observed); err != nil {
			return nil, err
		}
		values[machineCursorKey{node, iface}] = observed
	}
	return values, rows.Err()
}

// machineTrafficSummaries builds the current period of one node (only set) or of
// every live node, from one captured clock and panel zone.
func (r *Repository) machineTrafficSummaries(ctx context.Context, only string, now time.Time) ([]MachineTrafficSummary, string, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	zone, err := r.timezoneTx(ctx, tx)
	if err != nil {
		return nil, "", err
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, "", err
	}
	nodes, order, err := machineTrafficNodes(ctx, tx, only)
	if err != nil {
		return nil, "", err
	}
	stored, err := machineTrafficSettingsRows(ctx, tx, only)
	if err != nil {
		return nil, "", err
	}
	// One query covers the widest period of any node; each node then sums its
	// own window, which differs when the reset days differ.
	type window struct{ start, end time.Time }
	windows := map[string]window{}
	from, to := "", ""
	for _, id := range order {
		start, end := machinePeriod(machineSettingsFor(stored, id).ResetDay, now, loc)
		windows[id] = window{start, end}
		if from == "" || start.Format(MachineDayLayout) < from {
			from = start.Format(MachineDayLayout)
		}
		if end.Format(MachineDayLayout) > to {
			to = end.Format(MachineDayLayout)
		}
	}
	daily, err := machineDailyRows(ctx, tx, only, from, to)
	if err != nil {
		return nil, "", err
	}
	cursors, err := machineCursors(ctx, tx, only)
	if err != nil {
		return nil, "", err
	}
	items := make([]MachineTrafficSummary, 0, len(order))
	for _, id := range order {
		settings, span := machineSettingsFor(stored, id), windows[id]
		summary := MachineTrafficSummary{NodeID: id, Settings: settings, Timezone: zone, PeriodStart: span.start.Format(time.RFC3339), PeriodEnd: span.end.Format(time.RFC3339), NextResetAt: span.end.Format(time.RFC3339), Status: machineStatusUnknown}
		node := nodes[id]
		// The whole period is always recomputed from the one selected
		// interface; interfaces are never stitched together or summed.
		iface := settings.InterfaceName
		if iface == "" {
			iface = node.heartbeat.GetDefaultRouteInterface()
			if !ValidMachineTrafficInterface(iface, false) {
				iface = ""
			}
		}
		if iface == "" {
			items = append(items, summary)
			continue
		}
		summary.InterfaceName = &iface
		if observed, ok := cursors[machineCursorKey{id, iface}]; ok {
			summary.LastObservedAt = &observed
		}
		var rx, tx int64
		var rows int
		startDay, endDay := span.start.Format(MachineDayLayout), span.end.Format(MachineDayLayout)
		for _, row := range daily[id] {
			if row.iface != iface || row.day < startDay || row.day >= endDay {
				continue
			}
			var ok bool
			if rx, ok = addMachineBytes(rx, uint64(row.rx)); !ok {
				return nil, "", ErrMachineTrafficOverflow
			}
			if tx, ok = addMachineBytes(tx, uint64(row.tx)); !ok {
				return nil, "", ErrMachineTrafficOverflow
			}
			rows++
		}
		if rows == 0 {
			items = append(items, summary)
			continue
		}
		rxText, txText := strconv.FormatInt(rx, 10), strconv.FormatInt(tx, 10)
		used, err := machineUsed(settings.Method, rx, tx)
		if err != nil {
			return nil, "", err
		}
		summary.RxBytes, summary.TxBytes, summary.UsedBytes = &rxText, &txText, &used
		// Known rows stay visible while the server is offline or just rebooted;
		// only a fresh report of the same boot makes the total current.
		summary.Status = machineStatusStale
		if node.reported && freshHeartbeat(node.reportedAt, now) && ValidBootID(node.heartbeat.GetBootId()) && reportedInterface(node.heartbeat, iface) {
			summary.Status = machineStatusMeasured
		}
		items = append(items, summary)
	}
	return items, zone, tx.Commit()
}

// machineSettingsFor returns the stored settings of one node, or the defaults
// when it has never saved any.
func machineSettingsFor(stored map[string]MachineTrafficSettings, id string) MachineTrafficSettings {
	if settings, ok := stored[id]; ok {
		return settings
	}
	return defaultMachineTrafficSettings()
}

// reportedInterface reports whether one heartbeat measured the named interface.
func reportedInterface(heartbeat *pb.Heartbeat, name string) bool {
	for _, nic := range heartbeat.GetInterfaces() {
		if nic.GetName() == name {
			return true
		}
	}
	return false
}

// recordMachineTrafficTx accounts the host interface counters of one accepted
// heartbeat in the same transaction as the heartbeat itself, so a usage that
// cannot be stored is never acknowledged. A missing boot ID or interface list
// writes nothing and keeps the stored cursors.
func (r *Repository) recordMachineTrafficTx(ctx context.Context, tx *sql.Tx, id string, message *pb.AgentMessage, now time.Time) error {
	at, ok := freshObservationTime(message, now)
	if !ok {
		return nil
	}
	heartbeat := message.GetHeartbeat()
	boot := heartbeat.GetBootId()
	if !ValidBootID(boot) {
		return nil
	}
	samples := machineInterfaceSamples(heartbeat)
	if len(samples) == 0 {
		return nil
	}
	zone, err := r.timezoneTx(ctx, tx)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return err
	}
	for _, nic := range samples {
		cursor, found, err := readMachineCursor(ctx, tx, id, nic.GetName())
		if err != nil {
			return err
		}
		// A first sample — after the upgrade, for a new interface or after a
		// restore — is a baseline and never traffic since boot.
		if !found {
			if err := storeMachineCursor(ctx, tx, id, boot, nic, at); err != nil {
				return err
			}
			continue
		}
		// An observation at or before the cursor cannot be differenced, and a
		// changed boot or a decreasing counter is a discontinuity: rebaseline
		// without usage instead of retaining only the increasing direction.
		if !at.After(cursor.observedAt) {
			continue
		}
		if boot != cursor.boot || nic.ReceiveBytes < uint64(cursor.rx) || nic.TransmitBytes < uint64(cursor.tx) {
			if err := storeMachineCursor(ctx, tx, id, boot, nic, at); err != nil {
				return err
			}
			continue
		}
		// Days are bucketed in the panel zone current at ingestion; an interval
		// that spans an Agent or panel outage is split over those days.
		rxShares := machineDayShares(nic.ReceiveBytes-uint64(cursor.rx), cursor.observedAt, at, loc)
		txShares := machineDayShares(nic.TransmitBytes-uint64(cursor.tx), cursor.observedAt, at, loc)
		for day, rx := range rxShares {
			if err := addMachineDayTx(ctx, tx, id, nic.GetName(), day, rx, txShares[day]); err != nil {
				return err
			}
		}
		if err := storeMachineCursor(ctx, tx, id, boot, nic, at); err != nil {
			return err
		}
	}
	return nil
}

// machineInterfaceSamples returns the unique, valid non-loopback counters of one
// heartbeat. A name that appears twice is skipped: its two rows cannot be told
// apart. A counter above MaxInt64 is dropped rather than cast into a negative
// SQLite value, while other interfaces are still collected.
func machineInterfaceSamples(heartbeat *pb.Heartbeat) []*pb.InterfaceCounters {
	counts := map[string]int{}
	for _, nic := range heartbeat.GetInterfaces() {
		if nic != nil && ValidMachineTrafficInterface(nic.GetName(), false) {
			counts[nic.GetName()]++
		}
	}
	samples := []*pb.InterfaceCounters{}
	for _, nic := range heartbeat.GetInterfaces() {
		if nic == nil || counts[nic.GetName()] != 1 || nic.GetReceiveBytes() > math.MaxInt64 || nic.GetTransmitBytes() > math.MaxInt64 {
			continue
		}
		samples = append(samples, nic)
	}
	return samples
}

type machineCursor struct {
	boot       string
	rx         int64
	tx         int64
	observedAt time.Time
}

// readMachineCursor reads the stored counters one interface was last observed
// at.
func readMachineCursor(ctx context.Context, q rowQuery, id, iface string) (machineCursor, bool, error) {
	var cursor machineCursor
	var observed string
	err := q.QueryRowContext(ctx, "SELECT boot_id,rx_bytes,tx_bytes,observed_at FROM node_machine_traffic_cursors WHERE node_id=? AND interface_name=?", id, iface).Scan(&cursor.boot, &cursor.rx, &cursor.tx, &observed)
	if errors.Is(err, sql.ErrNoRows) {
		return machineCursor{}, false, nil
	}
	if err != nil {
		return machineCursor{}, false, err
	}
	cursor.observedAt, err = time.Parse(time.RFC3339Nano, observed)
	return cursor, true, err
}

func storeMachineCursor(ctx context.Context, tx *sql.Tx, id, boot string, nic *pb.InterfaceCounters, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO node_machine_traffic_cursors(node_id,interface_name,boot_id,rx_bytes,tx_bytes,observed_at) VALUES(?,?,?,?,?,?)
ON CONFLICT(node_id,interface_name) DO UPDATE SET boot_id=excluded.boot_id,rx_bytes=excluded.rx_bytes,tx_bytes=excluded.tx_bytes,observed_at=excluded.observed_at`, id, nic.GetName(), boot, int64(nic.GetReceiveBytes()), int64(nic.GetTransmitBytes()), timestamp(at))
	return err
}

// addMachineDayTx adds the shares of one observed interval to one daily row.
// Zero shares are written too, so a measured interval is never mistaken for a
// day without data.
func addMachineDayTx(ctx context.Context, tx *sql.Tx, id, iface, day string, rx, txBytes uint64) error {
	var currentRX, currentTX int64
	err := tx.QueryRowContext(ctx, "SELECT rx_bytes,tx_bytes FROM node_machine_traffic_daily WHERE node_id=? AND interface_name=? AND local_date=?", id, iface, day).Scan(&currentRX, &currentTX)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, "INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES(?,?,?,?,?)", id, iface, day, int64(rx), int64(txBytes))
		return err
	}
	if err != nil {
		return err
	}
	sumRX, rxOK := addMachineBytes(currentRX, rx)
	sumTX, txOK := addMachineBytes(currentTX, txBytes)
	if !rxOK || !txOK {
		return ErrMachineTrafficOverflow
	}
	_, err = tx.ExecContext(ctx, "UPDATE node_machine_traffic_daily SET rx_bytes=?,tx_bytes=? WHERE node_id=? AND interface_name=? AND local_date=?", sumRX, sumTX, id, iface, day)
	return err
}

// machineTraffic answers the current period of one live node. A deleted node is
// indistinguishable from a missing one.
func (h *Handler) machineTraffic(w http.ResponseWriter, r *http.Request) {
	items, _, err := h.Repository.machineTrafficSummaries(r.Context(), r.PathValue("id"), time.Now().UTC())
	if err != nil {
		h.fail(w, err)
		return
	}
	if len(items) != 1 {
		h.fail(w, ErrNotFound)
		return
	}
	httpapi.JSON(w, 200, items[0])
}

// machineTrafficBatch answers the list cards in one request: settings, cursors,
// heartbeats and daily rows are all batch-loaded, with one captured clock.
func (h *Handler) machineTrafficBatch(w http.ResponseWriter, r *http.Request) {
	items, zone, err := h.Repository.machineTrafficSummaries(r.Context(), "", time.Now().UTC())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "timezone": zone})
}

func (h *Handler) machineTrafficSettings(w http.ResponseWriter, r *http.Request) {
	n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.DeletedAt != "" {
		h.fail(w, ErrNotFound)
		return
	}
	settings, err := h.Repository.MachineTrafficSettings(r.Context(), n.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, settings)
}

// machineTrafficSettingsInput keeps an omitted allowance apart from an explicit
// null: a PUT always carries all four fields.
type machineTrafficSettingsInput struct {
	Method         *string         `json:"method"`
	InterfaceName  *string         `json:"interfaceName"`
	AllowanceBytes json.RawMessage `json:"allowanceBytes"`
	ResetDay       *int            `json:"resetDay"`
}

// putMachineTrafficSettings replaces the settings and records the change in the
// same transaction. Nothing is deployed: the settings only change which retained
// daily rows and direction form the displayed number.
func (h *Handler) putMachineTrafficSettings(w http.ResponseWriter, r *http.Request) {
	var body machineTrafficSettingsInput
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.Method == nil || body.InterfaceName == nil || body.ResetDay == nil || body.AllowanceBytes == nil {
		httpapi.Error(w, 400, "invalid_request", "method, interfaceName, allowanceBytes and resetDay are required")
		return
	}
	settings := MachineTrafficSettings{Method: *body.Method, InterfaceName: *body.InterfaceName, ResetDay: *body.ResetDay}
	if string(body.AllowanceBytes) != "null" {
		if err := json.Unmarshal(body.AllowanceBytes, &settings.AllowanceBytes); err != nil {
			httpapi.Error(w, 400, "invalid_request", "allowanceBytes must be an integer number of bytes or null")
			return
		}
	}
	if err := ValidateMachineTrafficSettings(settings); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.DeletedAt != "" {
		h.fail(w, ErrNotFound)
		return
	}
	tx, err := h.Repository.Database.BeginTx(r.Context(), nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	if err := saveMachineTrafficSettingsTx(r.Context(), tx, n.ID, settings); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.RecordTx(r.Context(), tx, auth.Actor(r.Context()).Username, "服务器流量设置", n.ID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, settings)
	if h.Notify != nil {
		h.Notify(n.ID)
	}
}
