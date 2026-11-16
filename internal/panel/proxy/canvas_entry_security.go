package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"proxycontrol/pkg/contracts/desiredstate"
)

// Discard creation-only inputs on a copy once the actual inbound owns its settings.
// This also keeps saved layouts valid for archive inspection after a stale edit.
func (s *Service) canvasIgnorePublishedEntrySecurity(ctx context.Context, schemeID string, layout CanvasLayout) (CanvasLayout, error) {
	layout.Instances = append([]CanvasInstance{}, layout.Instances...)
	for i := range layout.Instances {
		b := layout.Instances[i].EntryBinding
		if b == nil {
			continue
		}
		copy := *b
		_, err := s.Repository.CanvasEntryBinding(ctx, schemeID, layout.Instances[i].InstanceID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return layout, err
		}
		if err == nil {
			copy.Security, copy.Transport, copy.Flow = "", "", ""
			copy.RealityTarget, copy.RealityServerName = "", ""
		}
		layout.Instances[i].EntryBinding = &copy
	}
	return layout, nil
}

func validateCanvasEntrySecurity(b CanvasEntryBinding) error {
	if b.Protocol != "vless" {
		if b.Security != "" || b.Transport != "" || b.Flow != "" || b.RealityTarget != "" || b.RealityServerName != "" {
			return errors.New("入口安全设置仅支持 VLESS，其他协议不能填写这些字段")
		}
		return nil
	}
	if b.Security != "" && b.Security != "none" && b.Security != "tls" && b.Security != "reality" {
		return errors.New("VLESS 入口安全方式必须为 none、tls 或 reality")
	}
	if b.Transport != "" && b.Transport != "tcp" && b.Transport != "xhttp" {
		return errors.New("VLESS 入口传输必须为 tcp 或 xhttp")
	}
	secure := b.Security == "tls" || b.Security == "reality"
	if b.Transport == "xhttp" && !secure {
		return errors.New("xhttp 入口必须使用 TLS 或 Reality")
	}
	if b.Flow != "" && (b.Flow != "xtls-rprx-vision" || b.Transport == "xhttp" || !secure) {
		return errors.New("入口 Vision 仅支持 TCP + TLS 或 Reality")
	}
	if b.Security != "reality" {
		if b.RealityTarget != "" || b.RealityServerName != "" {
			return errors.New("非 Reality 入口不能填写握手目标或服务器名称")
		}
		return nil
	}
	settings := canvasEntrySettings(b)
	if err := settings.Reality.ValidateHandshake(); err != nil {
		return fmt.Errorf("Reality 入口握手目标（host:port）或服务器名称（单个具体 DNS 名称）无效：%w", err)
	}
	return nil
}

// Planning uses the same typed combination as SaveInbound, without creating secrets.
func canvasEntrySettings(b CanvasEntryBinding) desiredstate.ProtocolSettings {
	if b.Protocol != "vless" {
		return desiredstate.ProtocolSettings{}
	}
	settings := desiredstate.ProtocolSettings{Security: b.Security, Transport: b.Transport, Flow: b.Flow}
	if b.Security == "reality" {
		settings.Reality = &desiredstate.RealityServerSettings{HandshakeTarget: b.RealityTarget, ServerNames: []string{b.RealityServerName}}
	}
	if b.Transport == "xhttp" {
		settings.XHTTP = &desiredstate.XHTTPSettings{Mode: "auto", Path: "/"}
	}
	return settings
}

func canvasEntryRandomHex(size int) (string, error) {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func canvasEntrySecurityFields(b *CanvasEntryBinding, settings desiredstate.ProtocolSettings) {
	if b.Protocol != "vless" {
		return
	}
	b.Security, b.Transport, b.Flow = settings.Security, settings.Transport, settings.Flow
	if r := settings.Reality; r != nil {
		b.RealityTarget = r.HandshakeTarget
		if len(r.ServerNames) > 0 {
			b.RealityServerName = r.ServerNames[0]
		}
	}
}

func canvasEntryPlaintextWarning(name, protocol, security string) string {
	if protocol == "http" || protocol == "socks5" || protocol == "vless" && (security == "" || security == "none") {
		return fmt.Sprintf("入口「%s」使用不加密的 %s，容易被识别和干扰，不建议在国内直连使用", name, strings.ToUpper(protocol))
	}
	return ""
}
