package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
)

const realityPresetsKey = "reality_presets"

var ErrRealityPresetsInvalid = errors.New("Reality 预设无效")
var ErrRealityPresetsConflict = errors.New("Reality 预设已被修改，请刷新后再保存")

type RealityPreset struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	HandshakeTarget string   `json:"handshakeTarget"`
	ServerNames     []string `json:"serverNames"`
	Transport       string   `json:"transport"`
	Flow            string   `json:"flow"`
}

type RealityPresets struct {
	Items     []RealityPreset `json:"items"`
	DefaultID string          `json:"defaultId"`
	Revision  int64           `json:"revision"`
	UpdatedAt string          `json:"updatedAt"`
	UpdatedBy string          `json:"updatedBy"`
}

type RealityPresetsInput struct {
	Items            []RealityPreset `json:"items"`
	DefaultID        string          `json:"defaultId"`
	ExpectedRevision int64           `json:"expectedRevision"`
}

func normalizeRealityPresets(items []RealityPreset, defaultID string) ([]RealityPreset, error) {
	if len(items) > 20 {
		return nil, fmt.Errorf("%w：最多 20 个", ErrRealityPresetsInvalid)
	}
	normalized := make([]RealityPreset, 0, len(items))
	names, ids := map[string]bool{}, map[string]bool{}
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		if utf8.RuneCountInString(item.Name) < 1 || utf8.RuneCountInString(item.Name) > 64 || names[item.Name] {
			return nil, fmt.Errorf("%w：名称须为 1–64 个字符且不重名", ErrRealityPresetsInvalid)
		}
		names[item.Name] = true
		if item.ID != "" {
			if ids[item.ID] {
				return nil, fmt.Errorf("%w：ID 重复", ErrRealityPresetsInvalid)
			}
			ids[item.ID] = true
		}
		if item.Transport != "" && item.Transport != "tcp" && item.Transport != "xhttp" || item.Flow != "" && item.Flow != "xtls-rprx-vision" || item.Flow != "" && item.Transport != "tcp" {
			return nil, fmt.Errorf("%w：传输或 Flow 无效，Vision 只能配 TCP", ErrRealityPresetsInvalid)
		}
		if len(item.ServerNames) == 0 {
			host, _, err := net.SplitHostPort(item.HandshakeTarget)
			if err != nil {
				return nil, fmt.Errorf("%w：%v", ErrRealityPresetsInvalid, err)
			}
			item.ServerNames = []string{host}
		}
		handshake := desiredstate.RealityServerSettings{HandshakeTarget: item.HandshakeTarget, ServerNames: item.ServerNames}
		if err := handshake.ValidateHandshake(); err != nil {
			return nil, fmt.Errorf("%w：%v", ErrRealityPresetsInvalid, err)
		}
		normalized = append(normalized, item)
	}
	if defaultID != "" && !ids[defaultID] {
		return nil, fmt.Errorf("%w：默认预设不存在", ErrRealityPresetsInvalid)
	}
	return normalized, nil
}

func readRealityPresets(ctx context.Context, tx *sql.Tx) (RealityPresets, error) {
	value := RealityPresets{Items: []RealityPreset{}}
	var encoded string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", realityPresetsKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || decoder.Decode(&struct{}{}) != io.EOF || value.Revision < 1 {
		return RealityPresets{}, ErrRealityPresetsInvalid
	}
	items, err := normalizeRealityPresets(value.Items, value.DefaultID)
	if err != nil {
		return value, err
	}
	for _, item := range items {
		if item.ID == "" {
			return value, ErrRealityPresetsInvalid
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, value.UpdatedAt); err != nil {
		return value, ErrRealityPresetsInvalid
	}
	value.Items = items
	return value, nil
}

func (r *Repository) ReadRealityPresets(ctx context.Context) (RealityPresets, error) {
	tx, err := r.reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return RealityPresets{}, err
	}
	defer tx.Rollback()
	value, err := readRealityPresets(ctx, tx)
	if err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (r *Repository) SaveRealityPresets(ctx context.Context, input RealityPresetsInput, actor, ip string) (RealityPresets, error) {
	items, err := normalizeRealityPresets(input.Items, input.DefaultID)
	if err != nil {
		return RealityPresets{}, err
	}
	if input.ExpectedRevision < 0 || actor == "" {
		return RealityPresets{}, ErrRealityPresetsInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return RealityPresets{}, err
	}
	defer tx.Rollback()
	current, err := readRealityPresets(ctx, tx)
	if err != nil {
		return current, err
	}
	if input.ExpectedRevision != current.Revision || current.Revision == math.MaxInt64 {
		return current, ErrRealityPresetsConflict
	}
	known := map[string]bool{}
	for _, item := range current.Items {
		known[item.ID] = true
	}
	for i := range items {
		if items[i].ID != "" {
			if !known[items[i].ID] {
				return current, fmt.Errorf("%w：未知 ID", ErrRealityPresetsInvalid)
			}
		} else {
			items[i].ID, err = id.New()
			if err != nil {
				return current, err
			}
		}
	}
	if reflect.DeepEqual(items, current.Items) && input.DefaultID == current.DefaultID {
		return current, nil
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	next := RealityPresets{Items: items, DefaultID: input.DefaultID, Revision: current.Revision + 1, UpdatedAt: stamp, UpdatedBy: actor}
	data, err := json.Marshal(next)
	if err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", realityPresetsKey, string(data)); err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改 Reality 预设',?,'成功',?)", stamp, actor, realityPresetsKey, ip); err != nil {
		return next, err
	}
	return next, tx.Commit()
}
