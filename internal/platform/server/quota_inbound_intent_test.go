package server

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/vault"
)

// inboundIntentNames reports whether a stored inbound intent names an inbound.
func inboundIntentNames(intent []string, inboundID string) bool {
	for _, id := range intent {
		if id == inboundID {
			return true
		}
	}
	return false
}

// sameInboundIntent compares two stored intents, keeping "unknown" (nil) apart
// from an empty set.
func sameInboundIntent(left, right []string) bool {
	if (left == nil) != (right == nil) || len(left) != len(right) {
		return false
	}
	first := append([]string(nil), left...)
	second := append([]string(nil), right...)
	sort.Strings(first)
	sort.Strings(second)
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

// setInboundEnabled saves an inbound through the Owner's HTTP path with a new
// Enabled flag, which is exactly how the inbound page toggles it.
func (f *quotaFixture) setInboundEnabled(t *testing.T, inboundID string, enabled bool) {
	t.Helper()
	key, err := os.ReadFile(filepath.Join(f.app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	configurationVault, err := vault.FromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	record, err := (&proxy.Repository{Database: f.app.Database, Vault: configurationVault}).Inbound(f.ctx, inboundID, true)
	if err != nil {
		t.Fatal(err)
	}
	response := f.owner.call("PUT", "/api/v1/inbounds/"+inboundID, proxy.InboundInput{Name: record.Name, NodeID: record.NodeID, Protocol: record.Protocol, Listen: record.Listen, Port: record.Port, Enabled: enabled, SubscriptionVisible: record.SubscriptionVisible, DefaultOutbound: record.DefaultOutbound, AllowedUserIDs: record.AllowedUserIDs, Settings: record.Settings})
	if response.Code != 200 {
		t.Fatalf("save inbound: %d %s", response.Code, response.Body.String())
	}
}

// TestQuotaKeepsOwnerDisabledInboundDisabled is the regression: the Owner
// disables an inbound and publishes that, then enables it again only in the
// draft, then re-enables a user. The renderer disables an inbound no credential
// reaches, so the published state alone cannot tell such an inbound from the one
// the Owner disabled; without the recorded intent the automatic revision took
// the draft's Enabled and published an unreviewed Owner edit. A revision
// published before the intent existed (NULL) must behave the same way,
// conservatively.
func TestQuotaKeepsOwnerDisabledInboundDisabled(t *testing.T) {
	for name, forgetIntent := range map[string]bool{"owner disabled it": false, "revision published before the intent": true} {
		t.Run(name, func(t *testing.T) {
			f := newQuotaFixture(t)
			account, credential := f.addProtocolUser(t, "owner_disabled_"+strings.ReplaceAll(name, " ", "_"), "", "socks5")
			f.publish(t)
			f.quickAccess(t, "socks5", "0.0.0.0", 1080)
			inbound := f.publishedInbound(t, "socks5")

			// The Owner disables the inbound and publishes that decision.
			f.setInboundEnabled(t, inbound.ID, false)
			f.publish(t)
			if published := f.publishedInbound(t, "socks5"); published.Enabled {
				t.Fatalf("the published inbound is still enabled: %+v", published)
			}
			// The Owner enables it again, but only in the draft.
			f.setInboundEnabled(t, inbound.ID, true)

			// Disabling the user removes the credential automatically.
			account.Status = "disabled"
			saveUser(t, f, account, 0)
			flowWait(t, "the disable to remove the credential", func() bool { return len(f.autoRevisions(t)) == 1 })
			removed := f.autoRevisions(t)[0]
			f.apply(t, removed.ID)
			if forgetIntent {
				// What a revision created before this column existed reports.
				if _, err := f.app.Database.ExecContext(f.ctx, "UPDATE config_revisions SET inbound_enabled_json=NULL WHERE id=?", removed.ID); err != nil {
					t.Fatal(err)
				}
			}

			// Re-enabling the user restores the credential but must not enable
			// the inbound the Owner disabled.
			account.Status = "active"
			saveUser(t, f, account, 0)
			flowWait(t, "the re-enable to restore the credential", func() bool { return len(f.autoRevisions(t)) == 2 })
			state := f.state(t, f.autoRevisions(t)[1].ID)
			if kept := stateInbound(t, state, "socks5"); kept.Enabled || kept.ID != inbound.ID {
				t.Fatalf("the automatic revision published the draft's enable: %+v", kept)
			}
			if !stateHasCredential(state, credential) {
				t.Fatal("the re-enabled credential was not restored")
			}
		})
	}
}

// TestQuotaAutomaticRevisionInheritsInboundIntent pins that an automatic
// revision stores the published revision's intent unchanged, so the chain of
// automatic revisions stays anchored on what the Owner reviewed and a re-enable
// after several of them still restores an inbound the Owner wants enabled.
func TestQuotaAutomaticRevisionInheritsInboundIntent(t *testing.T) {
	f := newQuotaFixture(t)
	account, _ := f.addUser(t, "intent_inherit_user", "")
	baseline := f.publish(t)
	entry := f.publishedInbound(t, "vless")
	baseIntent, err := f.app.deployments.InboundIntent(f.ctx, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if !inboundIntentNames(baseIntent, entry.ID) {
		t.Fatalf("the renderer recorded %v, want the enabled inbound %s", baseIntent, entry.ID)
	}

	setQuota(t, f.ctx, f, account.ID, 50, 100)
	f.app.quotaDeployer.evaluate(f.ctx, []string{account.ID})
	flowWait(t, "the over-quota removal", func() bool { return len(f.autoRevisions(t)) == 1 })
	automatic := f.autoRevisions(t)[0]
	intent, err := f.app.deployments.InboundIntent(f.ctx, automatic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameInboundIntent(intent, baseIntent) {
		t.Fatalf("the automatic revision recorded %v, want the base revision's %v", intent, baseIntent)
	}
	// The over-quota state leaves the inbound without credentials, so the
	// revision disables it; the intent still says the Owner wants it, which is
	// what a later re-enable depends on.
	if kept := stateInbound(t, f.state(t, automatic.ID), "vless"); kept.Enabled {
		t.Fatalf("the credential-less inbound was not disabled: %+v", kept)
	}
	if !inboundIntentNames(intent, entry.ID) {
		t.Fatalf("the intent lost the owner-enabled inbound: %v", intent)
	}
}
