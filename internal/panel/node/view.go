package node

import (
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
)

// Keep the existing snake_case HTTP contract, adding camelCase K2 fields.
// Storage uses protojson throughout; optional zero values survive both forms.
func (n Node) MarshalJSON() ([]byte, error) {
	type legacy Node
	raw, err := json.Marshal(legacy(n))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	for _, kind := range []string{"capabilities", "heartbeat"} {
		var old map[string]json.RawMessage
		if err = json.Unmarshal(fields[kind], &old); err != nil {
			return nil, err
		}
		if old == nil {
			continue
		}
		var fresh []byte
		var keys []string
		if kind == "capabilities" {
			fresh, err = protojson.Marshal(n.Capabilities)
			keys = []string{"host", "tuningSupported"}
		} else {
			fresh, err = protojson.Marshal(n.Heartbeat)
			keys = []string{"load1", "load5", "load15", "processCount", "tcpConnections", "udpSockets", "memoryUsedBytes", "swapUsedBytes", "diskUsedBytes", "tuningCurrent"}
		}
		if err != nil {
			return nil, err
		}
		var camel map[string]json.RawMessage
		if err = json.Unmarshal(fresh, &camel); err != nil {
			return nil, err
		}
		for _, key := range keys {
			if value, ok := camel[key]; ok {
				old[key] = value
			}
		}
		if kind == "heartbeat" && len(n.Heartbeat.GetCores()) > 0 {
			cores := []map[string]any{}
			for _, c := range n.Heartbeat.Cores {
				if c == nil {
					continue
				}
				entry := map[string]any{"name": c.Name, "healthy": c.Healthy}
				if c.Error != "" {
					entry["error"] = c.Error
				}
				if c.StartedAt != nil {
					entry["started_at"] = c.StartedAt
				}
				if c.TrafficSourceStatus != nil {
					entry["traffic_source_status"] = c.TrafficSourceStatus
				}
				cores = append(cores, entry)
			}
			old["cores"], err = json.Marshal(cores)
			if err != nil {
				return nil, err
			}
		}
		fields[kind], err = json.Marshal(old)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}
