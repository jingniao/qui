package policy_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/panel/policy"
)

// v1.5.2: the periodic refresh fetches only sources of sets an enabled node
// policy names; Kick fetches a newly named set at once.
func TestRefresherFetchesOnlyReferencedSets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo, v := setupPolicyRepo(t)
	var mu sync.Mutex
	fetched := []string{}
	fetcher := &fakeFetcher{fetchFunc: func(_ context.Context, url string) ([]byte, error) {
		mu.Lock()
		fetched = append(fetched, url)
		mu.Unlock()
		if strings.Contains(url, "/geoip/") {
			return []byte("payload:\n  - 1.0.1.0/24\n"), nil
		}
		return []byte("payload:\n  - +.example.cn\n"), nil
	}}
	refresher := &policy.SourceRefresher{Repository: repo, Fetcher: fetcher, Vault: v}
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), fetched...)
	}

	service, err := repo.Service(ctx, "service-cn")
	if err != nil || service.AutoRefresh || len(service.Sources) != 2 {
		t.Fatal("built-in 国内 set", service, err)
	}
	done := make(chan struct{})
	go func() { refresher.Run(ctx); close(done) }()
	time.Sleep(300 * time.Millisecond)
	if got := snapshot(); len(got) != 0 {
		t.Fatal("unreferenced built-in sets were fetched", got)
	}

	if _, err := repo.Database.Exec("UPDATE policies SET enabled=1 WHERE id='policy-block-china'"); err != nil {
		t.Fatal(err)
	}
	refresher.Kick("service-cn")
	deadline := time.Now().Add(5 * time.Second)
	for len(snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := snapshot()
	if len(got) != 2 || !strings.HasSuffix(got[0], "geosite/geolocation-cn.yaml") || !strings.HasSuffix(got[1], "geoip/cn.yaml") {
		t.Fatal("kick did not fetch the 国内 sources", got)
	}
	service, err = repo.Service(ctx, "service-cn")
	if err != nil || !service.AutoRefresh {
		t.Fatal("referenced set not marked for refresh", err)
	}
	entries, err := repo.SourceEntries(ctx, "service-cn")
	if err != nil || len(entries.Suffixes) != 1 || len(entries.CIDRs) != 1 {
		t.Fatal(entries, err)
	}
	// Fetched sources are not due again; a second kick fetches nothing.
	refresher.Kick("service-cn")
	time.Sleep(300 * time.Millisecond)
	if got := snapshot(); len(got) != 2 {
		t.Fatal("fresh sources fetched again", got)
	}
	cancel()
	<-done
}
