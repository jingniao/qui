package recovery

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/panelstore"
	"time"
)

type FileInstallProgress struct {
	Version             int                      `json:"version"`
	ObservedAt          time.Time                `json:"observedAt"`
	TransactionID       string                   `json:"transactionId,omitempty"`
	Status              string                   `json:"status"`
	CommitAuthenticated bool                     `json:"commitAuthenticated"`
	ExecutionAvailable  bool                     `json:"executionAvailable"`
	OriginalMatches     int                      `json:"originalMatches"`
	NewMatches          int                      `json:"newMatches"`
	Indistinguishable   int                      `json:"indistinguishable"`
	ReviewRequired      int                      `json:"reviewRequired"`
	Entries             []FileInstallObservation `json:"entries"`
}
type FileInstallObservation struct {
	Position    int           `json:"position"`
	Destination string        `json:"destination"`
	Old         *archive.File `json:"old,omitempty"`
	New         *archive.File `json:"new,omitempty"`
	Status      string        `json:"status"`
}

// InspectFileInstall holds the same offline ownership as recovery, but never
// calls RecoverFiles, replaces targets, removes a journal or creates a key.
func InspectFileInstall(ctx context.Context, owner *panelstore.Ownership) (FileInstallProgress, error) {
	result := FileInstallProgress{Version: 1, ObservedAt: time.Now().UTC(), Status: "not_observed", Entries: []FileInstallObservation{}}
	if err := owner.AssertHeld(owner.Database, owner.IdentityDirectory); err != nil {
		return result, err
	}
	root := filepath.Dir(owner.Database)
	if _, err := os.Lstat(filepath.Join(root, journalName)); os.IsNotExist(err) {
		result.Status = "journal_missing_unknown"
		return result, nil
	} else if err != nil {
		return result, err
	}
	data, err := readPublishedInstallJournal(root)
	if os.IsNotExist(err) {
		result.Status = "journal_missing_unknown"
		return result, nil
	}
	if err != nil {
		return result, err
	}
	key, err := journalKey(owner)
	if err != nil {
		return result, err
	}
	defer clear(key)
	plan, err := openPlan(data, key)
	if err != nil {
		return result, err
	}
	work := filepath.Join(root, ".restore-txn-"+plan.ID)
	if err := verifyTransaction(ctx, owner, plan, work); err != nil {
		return result, err
	}
	if err := verifyPublishedInstallJournal(root, work, data); err != nil {
		return result, err
	}
	result.TransactionID = plan.ID
	result.Status = "uncommitted_materials_retained"
	if _, err := os.Lstat(filepath.Join(work, "committed")); err == nil {
		commit, e := readLimited(work, "committed", 128)
		if e != nil || string(commit) != commitMAC(data, key) {
			return result, ErrFileRecovery
		}
		result.CommitAuthenticated = true
		result.Status = "commit_record_authenticated"
	} else if !os.IsNotExist(err) {
		return result, err
	}
	mismatch := false
	for index, entry := range plan.Entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		observation := FileInstallObservation{Position: index + 1, Destination: entry.Destination, Old: entry.Old, New: entry.New}
		info, err := os.Lstat(entry.Destination)
		oldMatch, newMatch := false, false
		if os.IsNotExist(err) {
			oldMatch = entry.Old == nil
			newMatch = entry.New == nil
		} else if err != nil || !info.Mode().IsRegular() {
			observation.Status = "target_unavailable_or_changed"
		} else {
			limit := int64(0)
			if entry.Old != nil {
				limit = entry.Old.Bytes
			}
			if entry.New != nil && entry.New.Bytes > limit {
				limit = entry.New.Bytes
			}
			if info.Size() < 0 || info.Size() > limit {
				observation.Status = "target_unavailable_or_changed"
			} else {
				current, e := digest(ctx, entry.Destination, limit)
				if e != nil {
					if ctx.Err() != nil {
						return result, ctx.Err()
					}
					observation.Status = "target_unavailable_or_changed"
				} else {
					oldMatch = entry.Old != nil && current.Bytes == entry.Old.Bytes && current.SHA256 == entry.Old.SHA256
					newMatch = entry.New != nil && current.Bytes == entry.New.Bytes && current.SHA256 == entry.New.SHA256
				}
			}
		}
		if observation.Status == "" {
			switch {
			case oldMatch && newMatch:
				observation.Status = "same_old_and_new_unknown_step"
			case newMatch:
				observation.Status = "matches_new_material"
			case oldMatch:
				observation.Status = "matches_original_material"
			default:
				observation.Status = "target_unavailable_or_changed"
			}
		}
		if observation.Status == "target_unavailable_or_changed" || result.CommitAuthenticated && !newMatch {
			mismatch = true
		}
		switch observation.Status {
		case "matches_original_material":
			result.OriginalMatches++
		case "matches_new_material":
			result.NewMatches++
		case "same_old_and_new_unknown_step":
			result.Indistinguishable++
		default:
			result.ReviewRequired++
		}
		result.Entries = append(result.Entries, observation)
	}
	if mismatch {
		result.Status = "target_review_required"
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

// The published journal intentionally has two hardlinks. Read with a bound
// and verify both names refer to that same inode, rather than requiring the
// single-link rule used for private key and other original material files.
func readPublishedInstallJournal(root string) ([]byte, error) {
	return readPublishedInstallJournalAt(root, journalName)
}
func verifyPublishedInstallJournal(root, work string, data []byte) error {
	private, err := readPublishedInstallJournalAt(work, "journal.json")
	if err != nil || !bytes.Equal(data, private) {
		return ErrFileRecovery
	}
	publishedInfo, e := os.Lstat(filepath.Join(root, journalName))
	privateInfo, e2 := os.Lstat(filepath.Join(work, "journal.json"))
	if e != nil || e2 != nil || !publishedInfo.Mode().IsRegular() || !privateInfo.Mode().IsRegular() || !os.SameFile(publishedInfo, privateInfo) {
		return ErrFileRecovery
	}
	return nil
}
func readPublishedInstallJournalAt(directory, name string) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 2<<20 {
		return nil, ErrFileRecovery
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrFileRecovery
	}
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err != nil || int64(len(data)) != info.Size() {
		return nil, ErrFileRecovery
	}
	return data, nil
}
