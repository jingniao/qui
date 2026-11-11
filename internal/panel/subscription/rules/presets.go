package rules

import (
	"fmt"
	"strings"
)

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
		metaPreset("meta-lite", "精简分流 · 屏蔽回国", "节点选择、非中国与漏网之鱼三组；国内域名与国内 IP 一律拒绝，私网直连。", metaLiteServices),
		metaPreset("meta-standard", "标准分流 · 屏蔽回国", "在精简分流上增加 AI、YouTube、Google、Telegram、GitHub 分组；国内域名与国内 IP 一律拒绝。", metaStandardServices),
		metaPreset("meta-full", "完整分流 · 屏蔽回国", "按服务细分 20 个分组（广告、AI、流媒体、游戏、金融、云服务等）；国内域名与国内 IP 一律拒绝。", metaFullServices),
	}
}

// The three "屏蔽回国" presets (v1.5.2) follow the MetaCubeX meta-rules-dat
// layout: binary mrs rule sets, one select group per service. Unlike the
// usual layout they never send Chinese destinations DIRECT: Chinese domains
// (geolocation-cn, cn) and Chinese IP ranges are REJECTed outright, not routed
// through a group a client could switch back to DIRECT.
const metaRulesBase = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/"

const (
	metaSelect = "🚀 节点选择"
	metaAuto   = "♻️ 自动选择"
)

// metaService is one service group and the rule sets routed to it, in rule
// order. An "ip:" prefix marks a geoip set; those rules run after the domain
// rules with no-resolve, as in the reference layout.
type metaService struct {
	group string
	sets  []string
	// reject makes the group default to REJECT (the ad group) and runs its
	// rules before everything else.
	reject bool
}

var metaLiteServices = []metaService{}

var metaStandardServices = []metaService{
	{group: "🤖 AI 服务", sets: []string{"category-ai-!cn", "openai"}},
	{group: "📹 油管视频", sets: []string{"youtube"}},
	{group: "🔍 谷歌服务", sets: []string{"google", "ip:google"}},
	{group: "📲 电报消息", sets: []string{"telegram", "ip:telegram"}},
	{group: "🐱 Github", sets: []string{"github", "gitlab"}},
}

// metaFullServices follows the reference layout except for three sets the
// reference named but meta-rules-dat does not publish (404): applemusic is
// apple-music, proxy is gfw and proxymedia is category-media.
var metaFullServices = []metaService{
	{group: "🛑 广告拦截", sets: []string{"category-ads-all"}, reject: true},
	{group: "🤖 AI 服务", sets: []string{"category-ai-!cn", "openai"}},
	{group: "📹 油管视频", sets: []string{"youtube"}},
	{group: "🔍 谷歌服务", sets: []string{"google", "ip:google"}},
	{group: "📲 电报消息", sets: []string{"telegram", "ip:telegram"}},
	{group: "🐱 Github", sets: []string{"github", "gitlab"}},
	{group: "Ⓜ️ 微软服务", sets: []string{"microsoft"}},
	{group: "🍏 苹果服务", sets: []string{"apple", "apple-music"}},
	{group: "🌐 社交媒体", sets: []string{"facebook", "instagram", "twitter", "tiktok", "linkedin"}},
	{group: "🎬 流媒体", sets: []string{"netflix", "hulu", "disney", "hbo", "amazon", "bahamut", "biliintl"}},
	{group: "🎮 游戏平台", sets: []string{"steam", "epicgames", "ea", "ubisoft", "blizzard"}},
	{group: "📚 教育资源", sets: []string{"coursera", "edx", "udemy", "khanacademy", "category-scholar-!cn"}},
	{group: "💰 金融服务", sets: []string{"paypal", "visa", "mastercard", "stripe", "wise"}},
	{group: "☁️ 云服务", sets: []string{"aws", "azure", "digitalocean", "heroku", "dropbox", "cloudflare", "onedrive"}},
	{group: "🎵 Spotify", sets: []string{"spotify"}},
	{group: "🎨 Pixiv", sets: []string{"pixiv"}},
	{group: "📡 Abema", sets: []string{"abema"}},
	{group: "🔀 代理服务", sets: []string{"gfw"}},
	{group: "🎭 代理媒体", sets: []string{"category-media"}},
	{group: "🔞 E-Hentai", sets: []string{"ehentai"}},
}

// metaPreset builds one 屏蔽回国 preset. Rule order: ad block (if any),
// private networks, Chinese domains REJECT, service domain sets, everything
// else outside China, service IP sets, Chinese IPs REJECT, MATCH.
func metaPreset(id, name, description string, services []metaService) Preset {
	proxyMembers := []string{metaSelect, metaAuto, MemberAll, TargetDirect, TargetReject}
	groups := []Group{
		{Name: metaSelect, Type: "select", Members: []string{metaAuto, MemberAll, TargetDirect}},
		{Name: metaAuto, Type: "url-test", Members: []string{MemberAll}, TestURL: "https://www.gstatic.com/generate_204", IntervalSeconds: 300},
	}
	providers := []Provider{}
	addProvider := func(name, kind, file string) {
		behavior := "domain"
		if kind == "geoip" {
			behavior = "ipcidr"
		}
		providers = append(providers, Provider{Name: name, Behavior: behavior, Format: "mrs", URL: metaRulesBase + kind + "/" + file + ".mrs", IntervalSeconds: 86400})
	}
	rules := []Rule{}
	add := func(kind, payload, target string, noResolve bool, description string) {
		priority := (len(rules) + 1) * 10
		value := rule(fmt.Sprintf("cr-%d", len(rules)+1), kind, payload, target, priority, description)
		value.NoResolve = noResolve
		rules = append(rules, value)
	}
	ipRules := []metaService{}
	for _, service := range services {
		if service.reject {
			groups = append(groups, Group{Name: service.group, Type: "select", Members: []string{TargetReject, TargetDirect}})
			for _, set := range service.sets {
				addProvider(set, "geosite", set)
				add(RuleSet, set, service.group, false, service.group)
			}
		}
	}
	groups = append(groups, Group{Name: "🏠 私有网络", Type: "select", Members: []string{TargetDirect, metaSelect}})
	addProvider("private", "geoip", "private")
	add(RuleSet, "private", "🏠 私有网络", true, "私有网络直连")
	addProvider("geolocation-cn", "geosite", "geolocation-cn")
	addProvider("cn-domains", "geosite", "cn")
	add(RuleSet, "geolocation-cn", TargetReject, false, "屏蔽回国：国内域名")
	add(RuleSet, "cn-domains", TargetReject, false, "屏蔽回国：国内域名")
	for _, service := range services {
		if service.reject {
			continue
		}
		groups = append(groups, Group{Name: service.group, Type: "select", Members: proxyMembers})
		ips := metaService{group: service.group}
		for _, set := range service.sets {
			if strings.HasPrefix(set, "ip:") {
				ips.sets = append(ips.sets, strings.TrimPrefix(set, "ip:"))
				continue
			}
			addProvider(set, "geosite", set)
			add(RuleSet, set, service.group, false, service.group)
		}
		if len(ips.sets) > 0 {
			ipRules = append(ipRules, ips)
		}
	}
	groups = append(groups, Group{Name: "🌍 非中国", Type: "select", Members: proxyMembers})
	addProvider("geolocation-!cn", "geosite", "geolocation-!cn")
	add(RuleSet, "geolocation-!cn", "🌍 非中国", false, "非中国域名")
	for _, service := range ipRules {
		for _, set := range service.sets {
			name := set + "-ip"
			addProvider(name, "geoip", set)
			add(RuleSet, name, service.group, true, service.group+" IP")
		}
	}
	addProvider("cn", "geoip", "cn")
	add(RuleSet, "cn", TargetReject, true, "屏蔽回国：国内 IP")
	groups = append(groups, Group{Name: "🐟 漏网之鱼", Type: "select", Members: proxyMembers})
	add(RuleMatch, "", "🐟 漏网之鱼", false, "其余流量")
	return Preset{ID: id, Name: name, Description: description, Template: Template{
		Preset:    id,
		Client:    Client{Mode: "rule", MixedPort: 7890, LogLevel: "info"},
		DNS:       DNS{Enable: true, EnhancedMode: "fake-ip", Nameservers: []string{"https://doh.pub/dns-query", "https://dns.alidns.com/dns-query"}, Fallback: []string{"https://dns.cloudflare.com/dns-query", "https://dns.google/dns-query"}},
		Groups:    groups,
		Providers: providers,
		Rules:     rules,
	}}
}

// Default is the template used until the Owner saves one: the recommended
// preset.
func Default() Template {
	return Presets()[0].Template
}
