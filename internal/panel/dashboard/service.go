package dashboard

import (
	"context"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"time"
)

type Service struct {
	Repository *Repository
	Nodes      interface {
		List(context.Context, string, int, int) ([]node.Node, int, error)
		Get(context.Context, string) (node.Node, error)
	}
	Deployments interface {
		List(context.Context, int, int) ([]deployment.Revision, int, error)
	}
	Proxies interface {
		Chains(context.Context, string, int, int) ([]proxy.Chain, int, error)
	}
	Settings interface {
		ReadSetup(context.Context) (string, string, error)
	}
	Now func() time.Time
}
type View struct {
	GeneratedAt   time.Time             `json:"generatedAt"`
	Timezone      string                `json:"timezone"`
	DayStart      time.Time             `json:"dayStart"`
	Counts        Counts                `json:"counts"`
	Nodes         []node.Node           `json:"nodes"`
	Trend         []Hour                `json:"trend"`
	RecentAccess  []Recent              `json:"recentAccess,omitempty"`
	RecentBlocked []Recent              `json:"recentBlocked,omitempty"`
	Revisions     []deployment.Revision `json:"revisions,omitempty"`
	Chains        []proxy.Chain         `json:"chains,omitempty"`
	NodeNames     map[string]string     `json:"nodeNames,omitempty"`
}

func (s *Service) Read(ctx context.Context, owner bool) (View, error) {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	_, zone, err := s.Settings.ReadSetup(ctx)
	if err != nil {
		return View{}, err
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return View{}, err
	}
	local := now.In(location)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).UTC()
	to := now.Add(time.Second)
	view := View{GeneratedAt: now, Timezone: zone, DayStart: from}
	view.Counts, err = s.Repository.Counts(ctx, from, to, now)
	if err != nil {
		return view, err
	}
	view.Nodes, _, err = s.Nodes.List(ctx, "", 1, 5)
	if err != nil {
		return view, err
	}
	view.Trend, err = s.Repository.Trend(ctx, from, to)
	if err != nil {
		return view, err
	}
	if owner {
		view.RecentAccess, err = s.Repository.Recent(ctx, false)
		if err != nil {
			return view, err
		}
		view.RecentBlocked, err = s.Repository.Recent(ctx, true)
		if err != nil {
			return view, err
		}
		view.Revisions, _, err = s.Deployments.List(ctx, 1, 5)
		if err != nil {
			return view, err
		}
		view.Chains, _, err = s.Proxies.Chains(ctx, "", 1, 5)
		if err != nil {
			return view, err
		}
		view.NodeNames = map[string]string{}
		for _, n := range view.Nodes {
			view.NodeNames[n.ID] = n.Name
		}
		for _, chain := range view.Chains {
			for _, id := range chain.NodeIDs {
				if _, known := view.NodeNames[id]; !known {
					n, err := s.Nodes.Get(ctx, id)
					if err != nil {
						return view, err
					}
					view.NodeNames[id] = n.Name
				}
			}
		}

	}
	return view, err
}
