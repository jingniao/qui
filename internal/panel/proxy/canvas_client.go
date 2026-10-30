package proxy

import "errors"

// ValidateClientLayout accepts one directed path. Branches, disconnected boxes
// and cycles cannot be represented by a single dialer-proxy chain.
func ValidateClientLayout(layout CanvasLayout) error {
	if err := layout.validate(); err != nil {
		return err
	}
	if len(layout.Instances) < 2 || len(layout.Connections) != len(layout.Instances)-1 {
		return errors.New("客户端方案需要一条至少两跳的连续代理链")
	}
	instances := map[string]bool{}
	next := map[string]string{}
	parents := map[string]bool{}
	for _, i := range layout.Instances {
		ref, _ := parseCanvasRef(i.Ref)
		if ref.kind != "import" && ref.kind != "inbound" {
			return errors.New("客户端方案请选择导入节点或自建入站")
		}
		instances[i.InstanceID] = true
	}
	for _, c := range layout.Connections {
		if (c.Type != "proxy_chain" && c.Type != "tunnel") || parents[c.Target] || next[c.Source] != "" {
			return errors.New("客户端方案只支持单路径代理链")
		}
		parents[c.Target] = true
		next[c.Source] = c.Target
	}
	root := ""
	for id := range instances {
		if !parents[id] {
			if root != "" {
				return errors.New("客户端方案不能包含断开的链")
			}
			root = id
		}
	}
	seen := map[string]bool{}
	for root != "" {
		if seen[root] {
			return errors.New("客户端方案不能包含环路")
		}
		seen[root] = true
		root = next[root]
	}
	if len(seen) != len(instances) {
		return errors.New("客户端方案不能包含环路")
	}
	return nil
}
