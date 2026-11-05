package subscription

import (
	"context"
	"net/http"
	"strings"
	"time"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/platform/httpapi"
)

type Candidate struct {
	Ref          string   `json:"ref"`
	Name         string   `json:"name"`
	Protocol     string   `json:"protocol"`
	OriginKind   string   `json:"originKind"`
	OriginName   string   `json:"originName"`
	NodeGroupIDs []string `json:"nodeGroupIds"`
}

type Candidates struct {
	Items        []Candidate    `json:"items"`
	User         *candidateUser `json:"user,omitempty"`
	AutoSelected bool           `json:"autoSelected"`
	Diagnostic   string         `json:"diagnostic,omitempty"`
}

type candidateUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Service) Candidates(ctx context.Context, userID string) (Candidates, error) {
	result := Candidates{Items: []Candidate{}, AutoSelected: userID == ""}
	if userID == "" {
		for page := 1; ; page++ {
			users, total, err := s.CandidateUsers.List(ctx, "", page, 100)
			if err != nil {
				return result, err
			}
			for _, account := range users {
				if account.Active(time.Now()) && len(account.NodeGroupIDs) > 0 {
					userID = account.ID
					break
				}
			}
			if userID != "" || page*100 >= total {
				break
			}
		}
		if userID == "" {
			result.Diagnostic = "没有已授权节点组的有效用户"
			return result, nil
		}
	}
	account, viewer, err := s.account(ctx, userID, "")
	if err != nil {
		return result, err
	}
	endpoints, _, err := s.collect(ctx, viewer)
	if err != nil {
		return result, err
	}
	names, err := s.ImportNames.Names(ctx)
	if err != nil {
		return result, err
	}
	result.User = &candidateUser{ID: account.ID, Name: account.Username}
	seen := map[string]bool{}
	for _, endpoint := range endpoints {
		if endpoint.ClientChain != "" {
			continue
		}
		ref := rules.EndpointRef(endpoint)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		kind, _, _ := strings.Cut(ref, ":")
		name := endpoint.Origin.Node.Name
		if kind == "import" {
			name = names[strings.TrimPrefix(endpoint.Origin.Source, "import:")]
		}
		result.Items = append(result.Items, Candidate{Ref: ref, Name: endpoint.Name, Protocol: endpoint.Protocol, OriginKind: kind, OriginName: name, NodeGroupIDs: candidateGroups(endpoint.Origin)})
	}
	return result, nil
}

func candidateGroups(origin proxy.Origin) []string {
	groups := []string{}
	for _, label := range origin.Labels {
		if id, ok := strings.CutPrefix(label, "nodeGroup:"); ok {
			groups = append(groups, id)
		}
	}
	return groups
}

func (h *Handler) candidates(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.Candidates(r.Context(), r.URL.Query().Get("userId"))
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.JSON(w, http.StatusOK, result)
}
