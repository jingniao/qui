package proxy

import "strconv"

// OmissionWarnings groups endpoint names in first-seen reason order.
type OmissionWarnings struct {
	reasons []string
	names   map[string][]string
}

func (w *OmissionWarnings) Add(name, reason string) {
	if w.names == nil {
		w.names = map[string][]string{}
	}
	if _, ok := w.names[reason]; !ok {
		w.reasons = append(w.reasons, reason)
	}
	w.names[reason] = append(w.names[reason], name)
}

func OmissionText(reason, format string) string {
	switch reason {
	case "reality_spiderx":
		if format == "sing-box" {
			return "sing-box 不支持 Reality 的 spx 参数"
		}
		return "Clash 不支持 Reality 的 spx 参数"
	case "reality_no_vision":
		return "Reality 未使用 Vision"
	case "reality_grpc":
		return "Reality＋gRPC 暂不支持"
	case "xhttp_alpn":
		return "xhttp 的 ALPN 只支持留空或 h2"
	case "xhttp_mode":
		return "xhttp 模式不是 auto"
	case "pinned_ca":
		return "自签名证书无法写入链接，请改用 Clash 或 sing-box 订阅"
	case "client_chain":
		return "该客户端格式不支持客户端代理链"
	default:
		return "该客户端格式无法表达此节点的安全或传输设置"
	}
}

func (w *OmissionWarnings) Warnings(format string) []Warning {
	var result []Warning
	for _, reason := range w.reasons {
		names := w.names[reason]
		message := "节点"
		for i, name := range names {
			if i == 10 {
				break
			}
			message += "「" + name + "」"
		}
		if len(names) > 10 {
			message += "等 " + strconv.Itoa(len(names)) + " 个"
		}
		result = append(result, FormatWarning(format, message+"："+OmissionText(reason, format)))
	}
	return result
}
