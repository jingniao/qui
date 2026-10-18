package node

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "proxycontrol/pkg/contracts/agentpb"
)

var ErrToken = errors.New("invalid, expired or consumed enrollment token")
var ErrIdentity = errors.New("invalid or revoked node identity")
var ErrSequence = errors.New("message sequence is not consecutive")
var ErrNotFound = errors.New("node not found")

type Repository struct {
	Database         *sql.DB
	CertificateAudit interface {
		RecordNodeCertificate(context.Context, *sql.Tx, string, string) error
	}
}
type Node struct {
	AllowSubscription bool             `json:"allowSubscription"`
	Maintenance       bool             `json:"maintenance"`
	ID                string           `json:"id"`
	Name              string           `json:"name"`
	Status            string           `json:"status"`
	Enabled           bool             `json:"enabled"`
	Hostname          string           `json:"hostname"`
	OS                string           `json:"os"`
	AgentVersion      string           `json:"agentVersion"`
	PublicIP          string           `json:"publicIp"`
	OutboundSourceIP  string           `json:"outboundSourceIp"`
	OutboundInterface string           `json:"outboundInterface"`
	HostSourceIPv4    string           `json:"hostSourceIpv4"`
	HostSourceIPv6    string           `json:"hostSourceIpv6"`
	LastHeartbeat     string           `json:"lastHeartbeat"`
	LastSequence      uint64           `json:"lastSequence"`
	Capabilities      *pb.Capabilities `json:"capabilities"`
	Heartbeat         *pb.Heartbeat    `json:"heartbeat"`
	CreatedAt         string           `json:"createdAt"`
}

func timestamp(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func Hash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
func RandomID() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (r *Repository) CreateEnrollment(ctx context.Context, name string, duration time.Duration) (Node, string, time.Time, error) {
	id, err := RandomID()
	if err != nil {
		return Node{}, "", time.Time{}, err
	}
	token, err := RandomID()
	if err != nil {
		return Node{}, "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(duration)
	n := Node{ID: id, Name: name, Status: "pending", Enabled: true, AllowSubscription: true, CreatedAt: timestamp(now)}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Node{}, "", expires, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO nodes(id,name,status,created_at) VALUES(?,?,?,?)", id, name, "pending", n.CreatedAt); err != nil {
		return Node{}, "", expires, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO node_enrollment_tokens(token_hash,node_id,expires_at) VALUES(?,?,?)", Hash(token), id, timestamp(expires)); err != nil {
		return Node{}, "", expires, err
	}
	if err := tx.Commit(); err != nil {
		return Node{}, "", expires, err
	}
	return n, token, expires, nil
}

func (r *Repository) EnrollmentNode(ctx context.Context, hash string) (string, error) {
	var id string
	err := r.Database.QueryRowContext(ctx, "SELECT t.node_id FROM node_enrollment_tokens t JOIN nodes n ON n.id=t.node_id WHERE t.token_hash=? AND t.consumed_at IS NULL AND t.expires_at>? AND n.enabled=1", hash, timestamp(time.Now())).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrToken
	}
	return id, err
}

func (r *Repository) ConsumeEnrollment(ctx context.Context, hash, id, hostname, os, version, certificate, fingerprint string, expiry time.Time) error {
	return r.consumeEnrollment(ctx, hash, id, hostname, os, version, certificate, fingerprint, expiry, nil)
}
func (r *Repository) consumeEnrollment(ctx context.Context, hash, id, hostname, os, version, certificate, fingerprint string, expiry time.Time, receipt *enrollmentReceipt) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	consumedAt := timestamp(time.Now())
	if receipt != nil {
		consumedAt = receipt.CreatedAt
	}
	result, err := tx.ExecContext(ctx, "UPDATE node_enrollment_tokens SET consumed_at=? WHERE token_hash=? AND node_id=? AND consumed_at IS NULL AND expires_at>? AND EXISTS(SELECT 1 FROM nodes WHERE nodes.id=node_id AND enabled=1)", consumedAt, hash, id, timestamp(time.Now()))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrToken
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES(?,?,?,?)", id, certificate, fingerprint, timestamp(expiry)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET hostname=?,operating_system=?,agent_version=?,status='offline' WHERE id=? AND enabled=1", hostname, os, version, id); err != nil {
		return err
	}
	if receipt != nil {
		if _, err := tx.ExecContext(ctx, "INSERT INTO node_enrollment_receipts("+enrollmentReceiptColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", receipt.TokenHash, receipt.NodeID, receipt.RequestDigest, receipt.PublicKeyDigest, receipt.Hostname, receipt.OS, receipt.AgentVersion, receipt.CertificatePEM, receipt.Fingerprint, receipt.CAPEM, receipt.ExpiresAt, receipt.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) ValidateIdentity(ctx context.Context, id, fingerprint string) error {
	var enabled int
	err := r.Database.QueryRowContext(ctx, "SELECT n.enabled FROM node_identities i JOIN nodes n ON n.id=i.node_id WHERE i.node_id=? AND i.fingerprint=? AND i.revoked_at IS NULL AND i.expires_at>?", id, fingerprint, timestamp(time.Now())).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && enabled != 1 {
		return r.activateRenewal(ctx, id, fingerprint)
	}
	return err
}

func (r *Repository) LastSequence(ctx context.Context, id string) (uint64, error) {
	return scanSequence(r.Database.QueryRowContext(ctx, "SELECT "+sequenceMaterialSQL+" FROM nodes WHERE id=?", id))
}

func (r *Repository) AgentVersion(ctx context.Context, id, version string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE nodes SET agent_version=? WHERE id=?", version, id)
	return err
}

func (r *Repository) Record(ctx context.Context, id string, message *pb.AgentMessage) (uint64, error) {
	if message == nil || message.Sequence == 0 || message.Sequence > uint64(1<<63-1) {
		return 0, ErrSequence
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var last uint64
	var enabled bool
	if err := tx.QueryRowContext(ctx, "SELECT "+sequenceMaterialSQL+",CASE WHEN typeof(enabled)='integer' AND enabled IN (0,1) THEN enabled ELSE NULL END FROM nodes WHERE id=?", id).Scan(&last, &enabled); err != nil {
		return 0, err
	}
	if last > uint64(1<<63-1) {
		return 0, ErrSequence
	}
	if !enabled {
		return 0, ErrIdentity
	}
	if err := checkAcceptedMessageTx(ctx, tx, id, last, message); err != nil {
		return 0, err
	}
	if message.Sequence <= last {
		return last, nil
	}
	if message.Sequence != last+1 {
		return 0, ErrSequence
	}
	expected := last
	if heartbeat := message.GetHeartbeat(); heartbeat != nil {
		data, err := protojson.Marshal(heartbeat)
		if err != nil {
			return 0, err
		}
		status := "online"
		if heartbeat.CoreHealthy != nil && !heartbeat.GetCoreHealthy() {
			status = "degraded"
		}
		if err := sequenceRowsChanged(tx.ExecContext(ctx, "UPDATE nodes SET status=?,last_heartbeat=?,heartbeat_json=?,public_ip=CASE WHEN ?<>'' THEN ? ELSE public_ip END,last_sequence=? WHERE id=? AND enabled=1 AND last_sequence=?", status, timestamp(time.Now()), string(data), heartbeat.PublicIp, heartbeat.PublicIp, message.Sequence, id, last)); err != nil {
			return 0, err
		}
		expected = message.Sequence
	}
	if capabilities := message.GetCapabilities(); capabilities != nil {
		data, err := protojson.Marshal(capabilities)
		if err != nil {
			return 0, err
		}
		if err := sequenceRowsChanged(tx.ExecContext(ctx, "UPDATE nodes SET capabilities_json=?,last_sequence=? WHERE id=? AND enabled=1 AND last_sequence=?", string(data), message.Sequence, id, expected)); err != nil {
			return 0, err
		}
		expected = message.Sequence
	}
	if err := sequenceRowsChanged(tx.ExecContext(ctx, "UPDATE nodes SET last_sequence=? WHERE id=? AND enabled=1 AND last_sequence=?", message.Sequence, id, expected)); err != nil {
		return 0, err
	}
	digest, err := acceptedMessageDigest(message)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO node_accepted_messages(node_id,sequence,digest) VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET sequence=excluded.sequence,digest=excluded.digest", id, message.Sequence, digest); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return message.Sequence, nil
}

func (r *Repository) Offline(ctx context.Context, id string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE nodes SET status='offline' WHERE id=? AND status IN ('online','degraded')", id)
	return err
}
func (r *Repository) Update(ctx context.Context, id, name string, enabled bool) error {
	return r.UpdateSettings(ctx, id, SettingsInput{Name: name, Enabled: enabled})
}
func (r *Repository) Revoke(ctx context.Context, id string) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE node_identities SET revoked_at=? WHERE node_id=?", timestamp(time.Now()), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM node_certificate_renewals WHERE node_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

const columns = "id,name,status,enabled,hostname,operating_system,agent_version,public_ip,outbound_source_ip,outbound_interface,host_source_ipv4,host_source_ipv6,last_heartbeat,last_sequence,capabilities_json,heartbeat_json,created_at,allow_subscription,maintenance"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Node, error) {
	n := Node{Capabilities: &pb.Capabilities{}, Heartbeat: &pb.Heartbeat{}}
	var capabilities, heartbeat string
	err := row.Scan(&n.ID, &n.Name, &n.Status, &n.Enabled, &n.Hostname, &n.OS, &n.AgentVersion, &n.PublicIP, &n.OutboundSourceIP, &n.OutboundInterface, &n.HostSourceIPv4, &n.HostSourceIPv6, &n.LastHeartbeat, &n.LastSequence, &capabilities, &heartbeat, &n.CreatedAt, &n.AllowSubscription, &n.Maintenance)
	if err != nil {
		return n, err
	}
	if err := protojson.Unmarshal([]byte(capabilities), n.Capabilities); err != nil {
		return n, err
	}
	if err := protojson.Unmarshal([]byte(heartbeat), n.Heartbeat); err != nil {
		return n, err
	}
	if n.Status == "online" || n.Status == "degraded" {
		when, err := time.Parse(time.RFC3339Nano, n.LastHeartbeat)
		if err != nil || time.Since(when) > 45*time.Second {
			n.Status = "offline"
		}
	}
	return n, nil
}
func (r *Repository) Get(ctx context.Context, id string) (Node, error) {
	n, err := scan(r.Database.QueryRowContext(ctx, "SELECT "+columns+" FROM nodes WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return n, err
}
func (r *Repository) List(ctx context.Context, keyword string, page, size int) ([]Node, int, error) {
	var count int
	search := "%" + keyword + "%"
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM nodes WHERE name LIKE ? OR public_ip LIKE ?", search, search).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT "+columns+" FROM nodes WHERE name LIKE ? OR public_ip LIKE ? ORDER BY created_at,id LIMIT ? OFFSET ?", search, search, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Node{}
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, n)
	}
	return items, count, rows.Err()
}
