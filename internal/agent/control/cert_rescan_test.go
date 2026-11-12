package control

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestInventoryWatchReportsOnlyChanges(t *testing.T) {
	t.Parallel()
	watch := &inventoryWatch{}

	f1 := &pb.CertificateFileSummary{
		CertificatePath:   "/etc/proxy-agent/certs/a.crt",
		KeyPath:           "/etc/proxy-agent/certs/a.key",
		FingerprintSha256: "hash-a",
		NotAfter:          timestamppb.New(time.Unix(1000, 0)),
	}
	f2 := &pb.CertificateFileSummary{
		CertificatePath:   "/etc/proxy-agent/certs/b.crt",
		KeyPath:           "/etc/proxy-agent/certs/b.key",
		FingerprintSha256: "hash-b",
		NotAfter:          timestamppb.New(time.Unix(2000, 0)),
	}

	// 1. 首次调用只记录，返回 false
	if watch.changed([]*pb.CertificateFileSummary{f1}) {
		t.Fatal("expected first call to return false")
	}

	// 2. 相同文件，返回 false
	if watch.changed([]*pb.CertificateFileSummary{f1}) {
		t.Fatal("expected identical files to return false")
	}

	// 3. 新增文件，返回 true
	if !watch.changed([]*pb.CertificateFileSummary{f1, f2}) {
		t.Fatal("expected new file to return true")
	}

	// 4. 再次相同文件，返回 false
	if watch.changed([]*pb.CertificateFileSummary{f1, f2}) {
		t.Fatal("expected subsequent call with same files to return false")
	}
}
