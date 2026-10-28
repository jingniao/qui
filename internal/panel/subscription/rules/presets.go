package rules

// Preset is a built-in starting point. Applying one copies its template; the
// copy is not linked to the preset afterwards.
type Preset struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Recommended bool     `json:"recommended"`
	Template    Template `json:"template"`
}

const defaultTestURL = "http://www.gstatic.com/generate_204"

// defaultGroups are the groups every preset starts with. PROXY is the group the
// user picks a server in; AUTO and FALLBACK test the user's own servers. The
// earlier page filled these three with every node implicitly; @all says so
// explicitly and expands per user.
func defaultGroups() []Group {
	return []Group{
		{Name: "PROXY", Type: "select", Members: []string{"AUTO", "FALLBACK", MemberAll, TargetDirect}},
		{Name: "AUTO", Type: "url-test", Members: []string{MemberAll}, TestURL: defaultTestURL, IntervalSeconds: 300},
		{Name: "FALLBACK", Type: "fallback", Members: []string{MemberAll}, TestURL: defaultTestURL, IntervalSeconds: 300},
		{Name: "DOMESTIC", Type: "select", Members: []string{TargetDirect, "PROXY"}},
		{Name: "ADBLOCK", Type: "select", Members: []string{TargetReject, TargetDirect}},
	}
}

func presetTemplate(id string, rules []Rule) Template {
	return Template{
		Preset:    id,
		Client:    Client{Mode: "rule", MixedPort: 7890, AllowLAN: true, LogLevel: "info"},
		DNS:       DNS{Nameservers: []string{}, Fallback: []string{}},
		Groups:    defaultGroups(),
		Providers: []Provider{},
		Rules:     rules,
	}
}

func rule(id, kind, payload, target string, priority int, description string) Rule {
	return Rule{ID: id, Type: kind, Payload: payload, Target: target, Enabled: true, Priority: priority, Description: description}
}

func local(id, cidr string, priority int, description string) Rule {
	value := rule(id, RuleIPCIDR, cidr, TargetDirect, priority, description)
	value.NoResolve = true
	return value
}

// Presets lists the built-in presets, recommended first.
func Presets() []Preset {
	return []Preset{
		{ID: "standard", Name: "经典分流", Description: "国内直连，常见海外服务走代理，局域网与私网直接通信。", Recommended: true, Template: presetTemplate("standard", []Rule{
			local("cr-1", "127.0.0.0/8", 1, "本机回环"),
			local("cr-2", "192.168.0.0/16", 2, "局域网私网"),
			local("cr-3", "10.0.0.0/8", 3, "私网网段"),
			rule("cr-4", RuleDomainKeyword, "google", "PROXY", 10, "Google 走代理"),
			rule("cr-5", RuleDomainKeyword, "github", "PROXY", 11, "GitHub 走代理"),
			rule("cr-6", RuleDomainSuffix, "openai.com", "PROXY", 12, "OpenAI 走代理"),
			rule("cr-7", RuleGeoIP, "CN", TargetDirect, 50, "国内直连"),
			rule("cr-8", RuleMatch, "", "PROXY", 999, "漏网请求默认走代理"),
		})},
		{ID: "minimal", Name: "基础代理", Description: "仅私有地址直连，其余流量均通过 PROXY 代理分流。", Template: presetTemplate("minimal", []Rule{
			local("cr-1", "127.0.0.0/8", 1, "本机回环"),
			local("cr-2", "192.168.0.0/16", 2, "局域网"),
			rule("cr-3", RuleMatch, "", "PROXY", 999, "全量走代理"),
		})},
		{ID: "adblock", Name: "分流+去广告", Description: "在经典分流基础上增加常见广告域名与追踪域拦截 (REJECT)。", Template: presetTemplate("adblock", []Rule{
			rule("cr-1", RuleDomainKeyword, "adservice", TargetReject, 1, "广告拦截"),
			rule("cr-2", RuleDomainKeyword, "telemetry", TargetReject, 2, "遥测追踪拦截"),
			local("cr-3", "127.0.0.0/8", 5, "本机回环"),
			local("cr-4", "192.168.0.0/16", 6, "局域网"),
			rule("cr-5", RuleGeoIP, "CN", TargetDirect, 50, "国内直连"),
			rule("cr-6", RuleMatch, "", "PROXY", 999, "其余走代理"),
		})},
		{ID: "whitelist", Name: "白名单模式", Description: "仅指定站点走代理，其余全部 DIRECT 国内直连。", Template: presetTemplate("whitelist", []Rule{
			rule("cr-1", RuleDomainKeyword, "google", "PROXY", 1, "Google"),
			rule("cr-2", RuleDomainKeyword, "github", "PROXY", 2, "GitHub"),
			rule("cr-3", RuleDomainSuffix, "youtube.com", "PROXY", 3, "YouTube"),
			rule("cr-4", RuleDomainSuffix, "openai.com", "PROXY", 4, "OpenAI"),
			rule("cr-5", RuleMatch, "", TargetDirect, 999, "白名单模式: 其余全部直连"),
		})},
	}
}

// Default is the template used until the Owner saves one: the recommended
// preset.
func Default() Template {
	return Presets()[0].Template
}
