package subscription

import (
	"context"
	"testing"
	"time"
)

func TestRenewedBusinessUserDoesNotExportExpiredPublishedCredential(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.u.ExpiresAt = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	f.publish(t, f.nodeIDs[0])
	endpoints, err := f.service.Endpoints(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 0 {
		t.Fatal("business renewal bypassed expired published snapshot", len(endpoints), err)
	}
	f.u.ExpiresAt = time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	f.publish(t, f.nodeIDs[0])
	endpoints, err = f.service.Endpoints(ctx, f.u.ID, f.device.ID)
	if err != nil || len(endpoints) != 2 {
		t.Fatal("republished renewal did not restore subscription", len(endpoints), err)
	}
}
