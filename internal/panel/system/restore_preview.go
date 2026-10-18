package system

import (
	"context"
	"database/sql"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"time"
)

type RestoreImpact struct {
	UsersRemoved           int64 `json:"usersRemoved"`
	NodesRemoved           int64 `json:"nodesRemoved"`
	NodesAdded             int64 `json:"nodesAdded"`
	SequenceRegressions    int64 `json:"sequenceRegressions"`
	RuntimeDifferences     int64 `json:"runtimeDifferences"`
	CertificateDifferences int64 `json:"certificateDifferences"`
	RevocationRegressions  int64 `json:"revocationRegressions"`
	UsageRegressions       int64 `json:"usageRegressions"`
	UserReactivations      int64 `json:"userReactivations"`
}
type RestorePreview struct {
	BackupID           string         `json:"backupId"`
	ArchiveSHA256      string         `json:"archiveSha256"`
	CheckedAt          string         `json:"checkedAt"`
	Snapshot           archive.Totals `json:"snapshot"`
	Current            archive.Totals `json:"current"`
	Impact             RestoreImpact  `json:"impact"`
	SameKeyMaterial    bool           `json:"sameKeyMaterial"`
	SameNodeCA         bool           `json:"sameNodeCA"`
	Issues             []string       `json:"issues"`
	ExecutionAvailable bool           `json:"executionAvailable"`
}

func (s *BackupService) RestorePreview(ctx context.Context, backupID, password string) (preview RestorePreview, err error) {
	if err := archive.ValidatePassword(password); err != nil {
		return preview, err
	}
	done, err := s.beginStore(ctx)
	if err != nil {
		return preview, err
	}
	defer done()
	item, err := s.Repository.Backup(ctx, backupID)
	if err != nil {
		return preview, err
	}
	file, err := s.openChecked(ctx, item)
	if err != nil {
		return preview, err
	}
	file.Close()
	_, err = archive.Examine(ctx, s.Directory, filepath.Join(s.Directory, item.ID+".pcb"), password, func(result archive.Result, work string) error {
		if result.SHA256 != item.SHA256 || result.Bytes != item.FileBytes || result.Manifest.ID != item.ID {
			return ErrBackupFile
		}
		stored, err := archive.InventoryFromVerifiedDirectory(ctx, work)
		if err != nil {
			return err
		}
		snapshotMaterials, err := archive.ReadMaterials(filepath.Join(work, "identity"))
		if err != nil {
			return err
		}
		if err := s.Source.CheckConfigurationKey(); err != nil {
			return err
		}
		currentMaterials, err := archive.ReadMaterials(s.Source.Config.NodeIdentityDirectory)
		if err != nil {
			return err
		}
		// Read current business totals, usage and coordination from one SQLite
		// view. They remain observations, never a future execution permit.
		tx, err := s.Repository.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		current, err := archive.ReadInventory(ctx, tx)
		if err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		preview = compareRestore(stored, current)
		preview.BackupID = item.ID
		preview.ArchiveSHA256 = item.SHA256
		preview.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
		preview.SameKeyMaterial = snapshotMaterials.KeyDigest == currentMaterials.KeyDigest
		preview.SameNodeCA = snapshotMaterials.NodeCADigest == currentMaterials.NodeCADigest && snapshotMaterials.ActiveCADigest == currentMaterials.ActiveCADigest && snapshotMaterials.RetiredCADigest == currentMaterials.RetiredCADigest && snapshotMaterials.SuccessorCADigest == currentMaterials.SuccessorCADigest && snapshotMaterials.SuccessorRetiredCADigest == currentMaterials.SuccessorRetiredCADigest && snapshotMaterials.RecoveredRootBaseDigest == currentMaterials.RecoveredRootBaseDigest && snapshotMaterials.RecoveredAuthorityDigest == currentMaterials.RecoveredAuthorityDigest && snapshotMaterials.RecoveredRetirementDigest == currentMaterials.RecoveredRetirementDigest
		if !preview.SameKeyMaterial {
			preview.Issues = append(preview.Issues, "configuration_key_change")
		}
		if !preview.SameNodeCA {
			preview.Issues = append(preview.Issues, "node_ca_change")
		}
		return nil
	})
	if err != nil {
		return RestorePreview{}, err
	}
	return preview, nil
}

func compareRestore(snapshot, current archive.Inventory) RestorePreview {
	result := RestorePreview{Snapshot: snapshot.Totals, Current: current.Totals, Issues: []string{"restore_executor_pending"}}
	if snapshot.Totals.ActiveOwners != 1 {
		result.Issues = append(result.Issues, "active_owner_required")
	}
	if snapshot.Totals.Sessions > 0 || current.Totals.Sessions > 0 {
		result.Issues = append(result.Issues, "session_revocation_required")
	}
	if snapshot.Totals.PendingOperations > 0 {
		result.Issues = append(result.Issues, "snapshot_pending_deployments")
	}
	if snapshot.Totals.BackupFileOperations > 0 || current.Totals.BackupFileOperations > 0 {
		result.Issues = append(result.Issues, "backup_file_journal_reconciliation")
	}
	if snapshot.Totals.RunningBackupJobs > 0 || current.Totals.RunningBackupJobs > 0 {
		result.Issues = append(result.Issues, "backup_job_reconciliation")
	}
	if current.Totals.PendingOperations > 0 {
		result.Issues = append(result.Issues, "current_pending_deployments")
	}
	if snapshot.Totals.Nodes > 0 || current.Totals.Nodes > 0 {
		result.Issues = append(result.Issues, "node_reconciliation_required")
	}
	for id, user := range current.Users {
		old, exists := snapshot.Users[id]
		if !exists {
			result.Impact.UsersRemoved++
			if user.Used > 0 {
				result.Impact.UsageRegressions++
			}
			continue
		}
		if old.Used < user.Used {
			result.Impact.UsageRegressions++
		}
		if old.Status == "active" && user.Status != "active" {
			result.Impact.UserReactivations++
		}
	}
	for id, node := range current.Nodes {
		old, exists := snapshot.Nodes[id]
		if !exists {
			result.Impact.NodesRemoved++
			continue
		}
		if old.Sequence < node.Sequence {
			result.Impact.SequenceRegressions++
		}
		if old.RuntimeRevision != node.RuntimeRevision {
			result.Impact.RuntimeDifferences++
		}
	}
	for id := range snapshot.Nodes {
		if _, exists := current.Nodes[id]; !exists {
			result.Impact.NodesAdded++
		}
	}
	for id, identity := range current.Identities {
		old, exists := snapshot.Identities[id]
		if !exists || old.Fingerprint != identity.Fingerprint || old.Revoked != identity.Revoked {
			result.Impact.CertificateDifferences++
		}
		if exists && identity.Revoked && !old.Revoked {
			result.Impact.RevocationRegressions++
		}
	}
	for id := range snapshot.Identities {
		if _, exists := current.Identities[id]; !exists {
			result.Impact.CertificateDifferences++
		}
	}
	if result.Impact.SequenceRegressions > 0 {
		result.Issues = append(result.Issues, "message_cursor_regression")
	}
	if result.Impact.RevocationRegressions > 0 {
		result.Issues = append(result.Issues, "certificate_revocation_regression")
	}
	if result.Impact.UsageRegressions > 0 {
		result.Issues = append(result.Issues, "observed_usage_regression")
	}
	if result.Impact.UserReactivations > 0 {
		result.Issues = append(result.Issues, "user_reactivation")
	}
	return result
}
