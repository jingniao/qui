package proxy

import (
	"context"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestHealthOwnershipAtomicBatchAndNewerResultWins(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	outbound, err := f.service.SaveOutbound(ctx, "", OutboundInput{Name: "Exit", NodeID: f.nodeA, Protocol: "direct", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	checked := time.Now().UTC().Add(-time.Second)
	item := &pb.OutboundHealth{OutboundId: outbound.ID, Healthy: true, LatencyMs: 25, CheckedAt: timestamppb.New(checked)}
	batch := &pb.OutboundHealthBatch{Items: []*pb.OutboundHealth{item}}
	if err := f.service.Repository.RecordHealth(ctx, f.nodeB, batch); err == nil {
		t.Fatal("another node changed outbound health")
	}
	if err := f.service.Repository.RecordHealth(ctx, f.nodeA, batch); err != nil {
		t.Fatal(err)
	}
	old := &pb.OutboundHealth{OutboundId: outbound.ID, Healthy: false, LatencyMs: 0, CheckedAt: timestamppb.New(checked.Add(-time.Minute))}
	if err := f.service.Repository.RecordHealth(ctx, f.nodeA, &pb.OutboundHealthBatch{Items: []*pb.OutboundHealth{old}}); err != nil {
		t.Fatal(err)
	}
	status, err := f.service.Repository.Health(ctx, outbound.ID)
	if err != nil || status == nil || !status.Healthy || status.LatencyMS != 25 {
		t.Fatal("older batch overwrote newer health", status, err)
	}
	newer := &pb.OutboundHealth{OutboundId: outbound.ID, Healthy: false, CheckedAt: timestamppb.New(checked.Add(time.Second))}
	invalid := &pb.OutboundHealth{OutboundId: "missing", CheckedAt: timestamppb.Now()}
	if err := f.service.Repository.RecordHealth(ctx, f.nodeA, &pb.OutboundHealthBatch{Items: []*pb.OutboundHealth{newer, invalid}}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	status, _ = f.service.Repository.Health(ctx, outbound.ID)
	if !status.Healthy {
		t.Fatal("invalid batch partially committed")
	}
	for _, bad := range []*pb.OutboundHealthBatch{nil, {Items: []*pb.OutboundHealth{nil}}, {Items: []*pb.OutboundHealth{item, item}}, {Items: []*pb.OutboundHealth{{OutboundId: outbound.ID, CheckedAt: timestamppb.New(time.Now().Add(2 * time.Minute))}}}} {
		if err := f.service.Repository.RecordHealth(ctx, f.nodeA, bad); err == nil {
			t.Fatal("invalid health metadata accepted")
		}
	}
}
