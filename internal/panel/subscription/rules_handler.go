package subscription

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/platform/httpapi"
)

// RulesStore keeps the client rule template.
type RulesStore interface {
	Read(context.Context) (rules.Stored, error)
	Save(ctx context.Context, template rules.Template, expected int64, actor string) (rules.Stored, bool, error)
}

// ServiceRule is a panel service rule as a client rule provider sees it.
type ServiceRule struct {
	Enabled  bool
	Domains  []string
	Suffixes []string
	CIDRs    []string
}

// ServiceRules reads panel service rules. ErrServiceRuleNotFound reports an
// unknown id.
type ServiceRules interface {
	ServiceRule(ctx context.Context, id string) (ServiceRule, error)
}

// ErrServiceRuleNotFound reports a service rule that does not exist.
var ErrServiceRuleNotFound = errors.New("service rule not found")

// registerRules serves the rule template. A Viewer may read it (it holds no
// secret); only the Owner may change it.
func (h *Handler) registerRules(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/subscription-rules", h.Auth.Require(http.HandlerFunc(h.rulesRead), false))
	mux.Handle("PUT /api/v1/subscription-rules", h.Auth.Require(http.HandlerFunc(h.rulesSave), true))
	mux.Handle("GET /api/v1/subscription-rules/presets", h.Auth.Require(http.HandlerFunc(h.rulesPresets), false))
	mux.HandleFunc("GET /sub/{token}/rules/{id}", h.ruleProvider)
}

func (h *Handler) rulesRead(w http.ResponseWriter, r *http.Request) {
	if h.Rules == nil {
		httpapi.Error(w, 503, "unavailable", "Rule template unavailable")
		return
	}
	stored, err := h.Rules.Read(r.Context())
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, stored)
}

func (h *Handler) rulesSave(w http.ResponseWriter, r *http.Request) {
	if h.Rules == nil {
		httpapi.Error(w, 503, "unavailable", "Rule template unavailable")
		return
	}
	var body struct {
		Template         rules.Template `json:"template"`
		ExpectedRevision int64          `json:"expectedRevision"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	actor := auth.Actor(r.Context()).Username
	stored, changed, err := h.Rules.Save(r.Context(), body.Template, body.ExpectedRevision, actor)
	var field *rules.FieldError
	switch {
	case errors.As(err, &field):
		httpapi.ErrorDetails(w, 400, "invalid_rules", field.Message, map[string]any{"field": field.Field})
		return
	case errors.Is(err, rules.ErrConflict):
		httpapi.ErrorDetails(w, 409, "stale_rules", "规则模板已在其他地方修改，请刷新后再保存", map[string]any{"revision": stored.Revision})
		return
	case err != nil:
		h.internal(w, err)
		return
	}
	if changed {
		if err := h.Audit.Record(r.Context(), actor, "修改订阅规则模板", "client_rules", "成功", r.RemoteAddr); err != nil {
			h.internal(w, err)
			return
		}
	}
	httpapi.JSON(w, 200, stored)
}

func (h *Handler) rulesPresets(w http.ResponseWriter, r *http.Request) {
	httpapi.JSON(w, 200, map[string]any{"items": rules.Presets()})
}

// ruleProvider serves one panel service rule to a client as a mihomo rule
// provider in the classical text format. It is authenticated by the
// subscription token like /sub itself, and follows the account's status: a
// revoked token or an inactive account gets 403, never the rule content.
func (h *Handler) ruleProvider(w http.ResponseWriter, r *http.Request) {
	if h.ServiceRules == nil {
		httpapi.Error(w, 404, "not_found", "Rule set not found")
		return
	}
	token, err := h.Service.Repository.Resolve(r.Context(), r.PathValue("token"))
	if err == nil {
		_, _, err = h.Service.account(r.Context(), token.UserID, token.DeviceID)
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	service, err := h.ServiceRules.ServiceRule(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrServiceRuleNotFound) || (err == nil && !service.Enabled) {
		httpapi.Error(w, 404, "not_found", "Rule set not found")
		return
	}
	if err != nil {
		h.internal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(ClassicalRuleSet(service)))
}

// ClassicalRuleSet renders a service rule as mihomo classical provider lines.
// Entries a classical line cannot carry (a value with a comma or a line break,
// an invalid CIDR) are skipped rather than allowed to inject another rule.
func ClassicalRuleSet(service ServiceRule) string {
	lines := []string{}
	safe := func(value string) bool {
		return value != "" && !strings.ContainsAny(value, ",\r\n\t ")
	}
	for _, domain := range service.Domains {
		if safe(domain) {
			lines = append(lines, "DOMAIN,"+domain)
		}
	}
	for _, suffix := range service.Suffixes {
		if safe(suffix) {
			lines = append(lines, "DOMAIN-SUFFIX,"+strings.TrimPrefix(suffix, "."))
		}
	}
	for _, cidr := range service.CIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			address, addressErr := netip.ParseAddr(cidr)
			if addressErr != nil {
				continue
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		kind := "IP-CIDR"
		if prefix.Addr().Is6() && !prefix.Addr().Is4In6() {
			kind = "IP-CIDR6"
		}
		lines = append(lines, kind+","+prefix.Masked().String()+",no-resolve")
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// providerURLFor builds the link a client fetches a service rule from. It uses
// the scheme and host the client used for the subscription itself, so the
// profile points back at the same panel address.
func providerURLFor(r *http.Request, token string) func(string) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	return func(id string) string {
		return (&url.URL{Scheme: scheme, Host: host, Path: "/sub/" + token + "/rules/" + id}).String()
	}
}
