package importer

import (
	"context"
	"database/sql"
	"errors"
)

var ErrDeleteUnsupported = errors.New("订阅连接的节点跟随机场，不能单独删除")

type DeleteInput struct {
	ExpectedRevision int64 `json:"expectedRevision"`
	RemoveReferences bool  `json:"removeReferences"`
}
type DeletionNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
}
type DeletionGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type DeletionPolicy struct {
	ProfileID   string   `json:"profileId"`
	ProfileName string   `json:"profileName"`
	Groups      []string `json:"groups"`
	Active      bool     `json:"active"`
}
type DeletionCanvas struct {
	SchemeID   string `json:"schemeId"`
	SchemeName string `json:"schemeName"`
}
type DeletionPreview struct {
	Node           DeletionNode     `json:"node"`
	Groups         []DeletionGroup  `json:"groups"`
	ClientPolicies []DeletionPolicy `json:"clientPolicies"`
	Canvases       []DeletionCanvas `json:"canvases"`
	LastInSource   bool             `json:"lastInSource"`
}

// DeletionReferences keeps reference ownership in the respective modules while
// all reads and writes use the importer's transaction.
type DeletionReferences interface {
	Preview(context.Context, *sql.Tx, string, string) ([]DeletionGroup, []DeletionPolicy, []DeletionCanvas, error)
	Remove(context.Context, *sql.Tx, string, string) error
}
type DeletionConflict struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *DeletionConflict) Error() string { return e.Message }

func (s *Store) deletionPreviewTx(ctx context.Context, tx *sql.Tx, key string) (DeletionPreview, string, int64, error) {
	result := DeletionPreview{Groups: []DeletionGroup{}, ClientPolicies: []DeletionPolicy{}, Canvases: []DeletionCanvas{}}
	var src string
	var revision int64
	var sealed []byte
	err := tx.QueryRowContext(ctx, `SELECT p.id,p.name,p.protocol,p.source_id,p.revision,s.url_encrypted FROM imported_proxies p JOIN subscription_sources s ON s.id=p.source_id WHERE p.id=?`, key).Scan(&result.Node.ID, &result.Node.Name, &result.Node.Protocol, &src, &revision, &sealed)
	if err != nil {
		return result, src, revision, err
	}
	kind, err := s.sourceKindStrict(sealed)
	if err != nil {
		return result, src, revision, err
	}
	if kind != sourceKindContent {
		return result, src, revision, ErrDeleteUnsupported
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*)=1 FROM imported_proxies WHERE source_id=?", src).Scan(&result.LastInSource); err != nil {
		return result, src, revision, err
	}
	result.Groups, result.ClientPolicies, result.Canvases, err = s.DeletionReferences.Preview(ctx, tx, src, key)
	return result, src, revision, err
}

func (s *Store) DeletionPreview(ctx context.Context, key string) (DeletionPreview, error) {
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return DeletionPreview{}, err
	}
	defer tx.Rollback()
	result, _, _, err := s.deletionPreviewTx(ctx, tx, key)
	return result, err
}

func (s *Store) DeleteProxy(ctx context.Context, key string, in DeleteInput) error {
	s.refresh.Lock()
	defer s.refresh.Unlock()
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	preview, src, revision, err := s.deletionPreviewTx(ctx, tx, key)
	if err != nil {
		return err
	}
	if revision != in.ExpectedRevision {
		return ErrRenameStale
	}
	if len(preview.Canvases) > 0 {
		return &DeletionConflict{Code: "import_in_use_by_canvas", Message: "节点被画布引用，请先修改并发布方案", Details: map[string]any{"canvases": preview.Canvases}}
	}
	if !in.RemoveReferences && (len(preview.Groups) > 0 || len(preview.ClientPolicies) > 0) {
		return &DeletionConflict{Code: "import_referenced", Message: "节点被分组或客户端策略引用，请确认同时移除这些成员", Details: map[string]any{"groups": preview.Groups, "clientPolicies": preview.ClientPolicies}}
	}
	if in.RemoveReferences {
		if err = s.DeletionReferences.Remove(ctx, tx, src, key); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM imported_proxies WHERE id=?", key); err != nil {
		return err
	}
	if preview.LastInSource {
		if _, err = tx.ExecContext(ctx, "DELETE FROM subscription_sources WHERE id=?", src); err != nil {
			return err
		}
	}
	return tx.Commit()
}
