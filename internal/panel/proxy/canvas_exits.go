package proxy

import (
	"context"
	"errors"
)

// Inbound boxes identify a managed server as well as the protocol of an exit.
// Publication creates a dedicated internal listener; user credentials and the
// original inbound's subscription visibility are never changed.
func (s *Service) canvasManagedNode(ctx context.Context, instance CanvasInstance) (string, error) {
	ref, err := parseCanvasRef(instance.Ref)
	if err != nil {
		return "", err
	}
	switch ref.kind {
	case "server":
		return ref.id, nil
	case "inbound":
		in, err := s.Repository.Inbound(ctx, ref.id, true)
		return in.NodeID, err
	}
	return "", nil
}

func (s *Service) validateCanvasExits(ctx context.Context, layout CanvasLayout) error {
	instances := map[string]CanvasInstance{}
	for _, i := range layout.Instances {
		instances[i.InstanceID] = i
	}
	for _, c := range layout.Connections {
		source, target := instances[c.Source], instances[c.Target]
		sourceRef, _ := parseCanvasRef(source.Ref)
		if sourceRef.kind == "outbound" || sourceRef.kind == "inbound" && source.Role == "exit" {
			return errors.New("出口不能作为中转连接的起点")
		}
		ref, _ := parseCanvasRef(target.Ref)
		if ref.kind != "outbound" {
			continue
		}
		if c.Type == "port_forward" || c.TunnelProtocol != "" && c.TunnelProtocol != "tls" && c.TunnelProtocol != "wireguard" {
			return errors.New("外部代理出口只支持代理中转连接")
		}
		if !c.SupportTCP && !c.SupportUDP {
			return errors.New("连接至少需要支持 TCP 或 UDP")
		}
		out, err := s.Repository.Outbound(ctx, ref.id, false)
		if err != nil {
			return err
		}
		nodeID, err := s.canvasManagedNode(ctx, source)
		if err != nil {
			return err
		}
		if nodeID == "" || out.NodeID != nodeID {
			return errors.New("外部代理出口必须属于前一跳服务器")
		}
		present, err := s.Repository.canvasNodePresent(ctx, out.NodeID)
		if err != nil {
			return err
		}
		if !present || !out.Enabled || out.TargetNodeID != "" {
			return errors.New("外部代理出口不存在、已停用或不是外部代理")
		}
		switch out.Protocol {
		case "socks5", "http", "shadowsocks", "trojan", "vless":
		default:
			return errors.New("不支持的外部代理出口协议")
		}
		if c.SupportTCP && !out.SupportTCP || c.SupportUDP && !out.SupportUDP {
			return errors.New("外部代理出口不支持连接所需的 TCP/UDP")
		}
	}
	return nil
}
