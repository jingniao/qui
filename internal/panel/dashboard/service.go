package dashboard

import (
	"context"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/statistics"
	"time"
)

type Service struct {
	Statistics *statistics.Service
	Repository *Repository
	Nodes      interface {
		List(context.Context, string, int, int, bool) ([]node.Node, int, error)
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
type Base struct {
	GeneratedAt   time.Time             `json:"generatedAt"`
	Timezone      string                `json:"timezone"`
	DayStart      time.Time             `json:"dayStart"`
	Counts        BaseCounts            `json:"counts"`
	Nodes         []node.Node           `json:"nodes"`
	RecentAccess  []Recent              `json:"recentAccess,omitempty"`
	RecentBlocked []Recent              `json:"recentBlocked,omitempty"`
	Revisions     []deployment.Revision `json:"revisions,omitempty"`
	Chains        []proxy.Chain         `json:"chains,omitempty"`
	NodeNames     map[string]string     `json:"nodeNames,omitempty"`
}

type NodeTraffic = statistics.NodeTraffic
type Hour = statistics.Hour

// View preserves the published combined dashboard response.
type View struct {
	Base
	Counts      Counts        `json:"counts"`
	Trend       []Hour        `json:"trend"`
	NodeTraffic []NodeTraffic `json:"nodeTraffic"`
}

func (s *Service) Read(ctx context.Context, owner bool) (View, error) {
	base, err := s.readBase(ctx, owner, publishedDayStart)
	if err != nil {
		return View{}, err
	}
	// The published endpoint includes the current second. Its original result
	// requires no machine counters and must work independently of that worker.
	loc, err := time.LoadLocation(base.Timezone)
	if err != nil {
		return View{}, err
	}
	local := base.GeneratedAt.In(loc)
	monthStart := publishedDayStart(local.Year(), local.Month(), 1, loc).UTC()
	data, err := s.Repository.publishedTraffic(ctx, base.DayStart, monthStart, base.GeneratedAt.Add(time.Second))
	if err != nil {
		return View{}, err
	}
	return View{Base: base, Counts: Counts{BaseCounts: base.Counts, TodayUpload: data.TodayUpload, TodayDownload: data.TodayDownload, TrafficObservations: data.TrafficObservations}, Trend: data.Trend, NodeTraffic: data.NodeTraffic}, nil
}

func (s *Service) Base(ctx context.Context, owner bool) (Base, error) {
	return s.readBase(ctx, owner, node.LocalDayStart)
}

// The original combined endpoint used time.Date even in zones that skip midnight.
func publishedDayStart(year int, month time.Month, day int, loc *time.Location) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

func (s *Service) readBase(ctx context.Context, owner bool, dayStart func(int, time.Month, int, *time.Location) time.Time) (Base, error) {
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	_, zone, err := s.Settings.ReadSetup(ctx)
	if err != nil {
		return Base{}, err
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return Base{}, err
	}
	local := now.In(location)
	from := dayStart(local.Year(), local.Month(), local.Day(), location).UTC()
	to := now.Add(time.Second)
	view := Base{GeneratedAt: now, Timezone: zone, DayStart: from}
	view.Counts, err = s.Repository.Counts(ctx, from, to, now)
	if err != nil {
		return view, err
	}
	view.Nodes, _, err = s.Nodes.List(ctx, "", 1, 5, false)
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
