package node

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"google.golang.org/protobuf/proto"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func acceptedMessageDigest(message *pb.AgentMessage) (string, error) {
	if message == nil || message.Sequence == 0 || message.Sequence > uint64(1<<63-1) {
		return "", ErrSequence
	}
	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(message)
	if err != nil {
		return "", err
	}
	defer clear(data)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func readAcceptedMessage(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, prefix, nodeID string) (int64, string, error) {
	if prefix != "" && prefix != "restore_source." {
		return 0, "", ErrSequence
	}
	var sequence int64
	var digest string
	err := q.QueryRowContext(ctx, "SELECT CASE WHEN typeof(sequence)='integer' THEN sequence ELSE -1 END,CASE WHEN typeof(digest)='text' AND length(digest)=64 THEN digest ELSE '' END FROM "+prefix+"node_accepted_messages WHERE node_id=?", nodeID).Scan(&sequence, &digest)
	if err != nil {
		return 0, "", err
	}
	raw, e := hex.DecodeString(digest)
	if sequence < 1 || e != nil || len(raw) != 32 || hex.EncodeToString(raw) != digest {
		return 0, "", ErrSequence
	}
	return sequence, digest, nil
}
func checkAcceptedMessageTx(ctx context.Context, tx *sql.Tx, nodeID string, last uint64, message *pb.AgentMessage) error {
	sequence, digest, err := readAcceptedMessage(ctx, tx, "", nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if uint64(sequence) != last {
		return ErrSequence
	}
	if message.Sequence == last {
		actual, e := acceptedMessageDigest(message)
		if e != nil {
			return e
		}
		if actual != digest {
			return ErrSequence
		}
	}
	return nil
}
func (r *Repository) CheckAcceptedMessage(ctx context.Context, nodeID string, message *pb.AgentMessage) error {
	if message == nil || message.Sequence == 0 || message.Sequence > uint64(1<<63-1) || message.NodeId != nodeID {
		return ErrSequence
	}
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	last, err := scanSequence(tx.QueryRowContext(ctx, "SELECT "+sequenceMaterialSQL+" FROM nodes WHERE id=?", nodeID))
	if err != nil {
		return err
	}
	return checkAcceptedMessageTx(ctx, tx, nodeID, last, message)
}
func ValidateAcceptedMessageMaterial(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) error {
	return validateAcceptedMessageMaterial(ctx, q, "")
}
func validateAcceptedMessageMaterial(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, prefix string) error {
	if prefix != "" && prefix != "restore_source." {
		return ErrSequence
	}
	var present, applied int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+prefix+"sqlite_master WHERE type='table' AND name='node_accepted_messages'").Scan(&present); err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+prefix+"schema_migrations WHERE version='0066_node_accepted_messages.sql'").Scan(&applied); err != nil {
		return err
	}
	if present == 0 && applied == 0 {
		return nil
	}
	if present != 1 || applied != 1 {
		return ErrSequence
	}
	rows, err := q.QueryContext(ctx, "SELECT a.node_id,CASE WHEN typeof(a.sequence)='integer' THEN a.sequence ELSE -1 END,CASE WHEN typeof(a.digest)='text' AND length(a.digest)=64 THEN a.digest ELSE '' END,CASE WHEN typeof(n.last_sequence)='integer' THEN n.last_sequence ELSE -1 END FROM "+prefix+"node_accepted_messages a LEFT JOIN "+prefix+"nodes n ON n.id=a.node_id ORDER BY a.node_id LIMIT 100001")
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	previous := ""
	for rows.Next() {
		var nodeID, digest string
		var sequence, last int64
		if err := rows.Scan(&nodeID, &sequence, &digest, &last); err != nil {
			return err
		}
		raw, e := hex.DecodeString(digest)
		count++
		if count > 100000 || nodeID <= previous || len(nodeID) > 128 || sequence < 1 || sequence != last || e != nil || len(raw) != 32 || hex.EncodeToString(raw) != digest {
			return ErrSequence
		}
		previous = nodeID
	}
	return rows.Err()
}
func preserveAcceptedMessageMaterialTx(ctx context.Context, tx *sql.Tx) error {
	if err := validateAcceptedMessageMaterial(ctx, tx, ""); err != nil {
		return err
	}
	if err := validateAcceptedMessageMaterial(ctx, tx, "restore_source."); err != nil {
		return err
	}
	var source int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM restore_source.sqlite_master WHERE type='table' AND name='node_accepted_messages'").Scan(&source); err != nil {
		return err
	}
	if source == 0 {
		return db.AssertRecoveryNone(ctx, tx, "SELECT count(*) FROM node_accepted_messages")
	}
	return db.AssertRecoveryNone(ctx, tx, "SELECT count(*) FROM node_accepted_messages s LEFT JOIN restore_source.node_accepted_messages c ON c.node_id=s.node_id WHERE c.node_id IS NULL OR c.sequence<s.sequence OR c.sequence=s.sequence AND c.digest IS NOT s.digest")
}
