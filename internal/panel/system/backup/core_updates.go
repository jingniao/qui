package backup

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"time"

	"proxycontrol/pkg/contracts/release"
)

func validateCoreUpdates(ctx context.Context, database *sql.DB) error {
	releases, err := catalogueReleases(ctx, database)
	if err != nil {
		return err
	}
	// A GOST task can only use the reviewed pin. The node's live Hello flag is
	// not re-checked against the row: it is cleared by a restore while an
	// unconfirmed task survives, so requiring it would make such an archive
	// unvalidatable.
	rows, err := database.QueryContext(ctx, "SELECT id,node_id,baseline_revision,release_id,component,version,url,sha256,sequence,status,command_id,result_json FROM core_update_tasks")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var id, nodeID, component, version, url, digest, status, commandID string
		var entryID sql.NullString
		var revision, sequence int64
		var resultData []byte
		if rows.Scan(&id, &nodeID, &revision, &entryID, &component, &version, &url, &digest, &sequence, &status, &commandID, &resultData) != nil {
			return ErrArchive
		}
		if !release.CoreComponent(component) || sequence < 1 || revision < 1 || !release.ValidUpdateID(commandID) {
			return ErrArchive
		}
		if component == "gost" && !release.CorePinnedTarget(component, version, digest) {
			return ErrArchive
		}
		if component == "gost" {
			if _, _, err := release.CoreAssetURL(component, url); err != nil {
				return ErrArchive
			}
		}
		switch status {
		case "queued", "running", "success", "rolled_back", "rejected", "failed_closed", "cleared", "superseded":
		default:
			return ErrArchive
		}
		if entryID.Valid {
			// A historical task keeps its signed catalogue binding.
			entry, ok := releases[entryID.String]
			if !ok || entry.Manifest.Component != component {
				return ErrArchive
			}
		} else if !release.StableCoreVersion(version) {
			return ErrArchive
		}
		if len(resultData) == 0 {
			continue
		}
		result, err := release.DecodeCoreUpdateResult(resultData)
		if err != nil || result.ID != commandID || result.NodeID != nodeID || result.Revision != revision || result.TargetDigest != digest {
			return ErrArchive
		}
		if result.Status == "success" && result.InstalledVersion != version {
			return ErrArchive
		}
		if !coreResultMatches(status, result.Status) {
			return ErrArchive
		}
	}
	return rows.Err()
}

// coreResultMatches reports whether a stored Agent result may belong to a task
// status. A task the Owner cleared after the node reported is finished: its
// result was stored first and keeps the terminal status the node sent, so it no
// longer equals 'cleared'. Both decoders above already refuse a result that is
// not terminal. Reinstallation also keeps the original result under superseded.
// Every other task status stores exactly its own result status.
func coreResultMatches(status, resultStatus string) bool {
	return status == "cleared" || status == "superseded" || status == resultStatus
}

func catalogueReleases(ctx context.Context, database *sql.DB) (map[string]release.Signed, error) {
	rows, err := database.QueryContext(ctx, "SELECT id,component,operating_system,architecture,release_sequence,version,digest,signed_json,signing_public_key,created_at FROM release_catalogue")
	if err != nil {
		return nil, ErrArchive
	}
	defer rows.Close()
	releases := map[string]release.Signed{}
	for rows.Next() {
		var id, component, platform, arch, version, digest, pin, created string
		var sequence int64
		var data []byte
		if rows.Scan(&id, &component, &platform, &arch, &sequence, &version, &digest, &data, &pin, &created) != nil {
			return nil, ErrArchive
		}
		signed, err := release.Decode(data)
		key, keyErr := base64.StdEncoding.DecodeString(pin)
		at, timeErr := time.Parse(time.RFC3339Nano, created)
		if err != nil || keyErr != nil || len(key) != ed25519.PublicKeySize || timeErr != nil || signed.Manifest.Component != component || signed.Manifest.OS != platform || signed.Manifest.Architecture != arch || signed.Manifest.Sequence != sequence || signed.Manifest.Version != version || signed.Manifest.SHA256 != digest || signed.Verify(release.Policy{Component: component, OS: platform, Architecture: arch, Now: at, Keys: map[string]release.TrustedKey{signed.KeyID: {PublicKey: key, Components: []string{component}}}}) != nil {
			return nil, ErrArchive
		}
		releases[id] = signed
	}
	return releases, rows.Err()
}
