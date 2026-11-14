package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

var canvasBaselineFields = []string{"名称", "协议", "监听地址", "端口", "核心", "协议设置", "启用", "订阅可见", "允许用户"}

// Store only digests of editable configuration. User credentials and dynamically
// authorized users are not part of an inbound's persisted settings.
func canvasEntryBaseline(in Inbound) (string, error) {
	users := append([]string{}, in.AllowedUserIDs...)
	slices.Sort(users)
	users = slices.Compact(users)
	settings := in.Settings
	settings.CertificatePEM = strings.TrimSpace(strings.ReplaceAll(settings.CertificatePEM, "\r\n", "\n"))
	settings.PrivateKeyPEM = strings.TrimSpace(strings.ReplaceAll(settings.PrivateKeyPEM, "\r\n", "\n"))
	settings.TrustedCAPEM = strings.TrimSpace(strings.ReplaceAll(settings.TrustedCAPEM, "\r\n", "\n"))
	values := []any{in.Name, in.Protocol, in.Listen, in.Port, in.Core,
		struct {
			Settings                 any
			TLSSource, CertificateID string
		}{settings, in.TLSSource, in.CertificateID},
		in.Enabled, in.SubscriptionVisible, users}
	digests := map[string]string{}
	for i, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		digests[canvasBaselineFields[i]] = hex.EncodeToString(sum[:])
	}
	data, err := json.Marshal(digests)
	return string(data), err
}

func canvasBaselineGroups(groups []CanvasPlanEntryGroup) string {
	ids := []string{}
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	data, _ := json.Marshal(ids)
	return string(data)
}

// ValidateCanvasEntryBaseline is shared with archive inspection. NULL pairs are
// historical bindings; partial, unknown or noncanonical snapshots are invalid.
func ValidateCanvasEntryBaseline(config, groups *string) error {
	invalid := errors.New("invalid canvas entry creation baseline")
	if config == nil && groups == nil {
		return nil
	}
	if config == nil || groups == nil {
		return invalid
	}
	var fields map[string]string
	if json.Unmarshal([]byte(*config), &fields) != nil || len(fields) != len(canvasBaselineFields) {
		return invalid
	}
	for _, key := range canvasBaselineFields {
		value := fields[key]
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != sha256.Size || value != strings.ToLower(value) {
			return invalid
		}
	}
	canonical, err := json.Marshal(fields)
	if err != nil || string(canonical) != *config {
		return invalid
	}
	var ids []string
	if json.Unmarshal([]byte(*groups), &ids) != nil || ids == nil {
		return invalid
	}
	for i, id := range ids {
		if strings.TrimSpace(id) != id || id == "" || strings.ContainsAny(id, "\r\n\x00") || (i > 0 && ids[i-1] >= id) {
			return invalid
		}
	}
	canonical, err = json.Marshal(ids)
	if err != nil || string(canonical) != *groups {
		return invalid
	}
	return nil
}

func canvasEntryBaselineChanges(b CanvasEntryBindingRecord, in Inbound, groups []CanvasPlanEntryGroup) ([]string, error) {
	changes := []string{}
	if err := ValidateCanvasEntryBaseline(b.CreationConfigJSON, b.CreationGroupsJSON); err != nil {
		return nil, err
	}
	if b.CreationConfigJSON == nil {
		changes = append(changes, "未记录创建时完整配置，须确认删除当前协议、端口及其他配置")
		for _, g := range groups {
			changes = append(changes, "当前分组需确认移除："+g.Name+"（"+g.ID+"）")
		}
		return changes, nil
	}
	current, err := canvasEntryBaseline(in)
	if err != nil {
		return nil, err
	}
	var before, after map[string]string
	if err := json.Unmarshal([]byte(*b.CreationConfigJSON), &before); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(current), &after); err != nil {
		return nil, err
	}
	for _, key := range canvasBaselineFields {
		if before[key] != after[key] {
			changes = append(changes, key+"已修改")
		}
	}
	var original []string
	if err := json.Unmarshal([]byte(*b.CreationGroupsJSON), &original); err != nil {
		return nil, err
	}
	for _, g := range groups {
		if !slices.Contains(original, g.ID) {
			changes = append(changes, "新增分组："+g.Name+"（"+g.ID+"）")
		}
	}
	for _, id := range original {
		if !slices.ContainsFunc(groups, func(g CanvasPlanEntryGroup) bool { return g.ID == id }) {
			changes = append(changes, "移出分组："+id)
		}
	}
	return changes, nil
}
