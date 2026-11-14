package importer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"proxycontrol/internal/panel/subscription/proxy"
)

var (
	ErrRenameUnsupported = errors.New("subscription URL nodes cannot be renamed")
	ErrRenameName        = errors.New("invalid imported proxy name")
	ErrRenameStale       = errors.New("imported proxy revision changed")
)

type RenameInput struct {
	Name             string `json:"name"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type NameConflict struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type NameConflictError struct{ Conflicts []NameConflict }

func (e *NameConflictError) Error() string {
	return "imported proxy name conflicts with existing endpoints"
}

func importedItem(key, src, name, protocol, kind string, revision int64, p proxy.Proxy) map[string]any {
	proxyKey := name + "|" + protocol
	if kind == sourceKindContent {
		proxyKey = "id:" + key
	}
	return map[string]any{"id": key, "sourceId": src, "proxyKey": proxyKey, "name": name, "protocol": protocol, "server": p.Server, "port": p.Port, "kind": kind, "revision": revision}
}

func (s *Store) Rename(ctx context.Context, key string, in RenameInput) (map[string]any, error) {
	in.Name = strings.TrimSpace(in.Name)
	if len(in.Name) == 0 || len(in.Name) > 200 || !utf8.ValidString(in.Name) || strings.ContainsFunc(in.Name, unicode.IsControl) {
		return nil, ErrRenameName
	}
	s.refresh.Lock()
	defer s.refresh.Unlock()
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var src, protocol string
	var sealed, sealedURL []byte
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT p.source_id,p.protocol,p.revision,p.payload_encrypted,s.url_encrypted FROM imported_proxies p JOIN subscription_sources s ON s.id=p.source_id WHERE p.id=?`, key).Scan(&src, &protocol, &revision, &sealed, &sealedURL); err != nil {
		return nil, err
	}
	kind, err := s.sourceKindStrict(sealedURL)
	if err != nil {
		return nil, err
	}
	if kind != sourceKindContent {
		return nil, ErrRenameUnsupported
	}
	if revision != in.ExpectedRevision {
		return nil, ErrRenameStale
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.name,s.url_encrypted FROM imported_proxies p JOIN subscription_sources s ON s.id=p.source_id WHERE p.name=? AND p.id<>? ORDER BY p.source_id,p.id`, in.Name, key)
	if err != nil {
		return nil, err
	}
	conflicts := []NameConflict{}
	for rows.Next() {
		var name string
		var url []byte
		if err = rows.Scan(&name, &url); err != nil {
			rows.Close()
			return nil, err
		}
		kind, err := s.sourceKindStrict(url)
		if err != nil {
			rows.Close()
			return nil, err
		}
		conflicts = append(conflicts, NameConflict{Type: kind, Name: name})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if s.SelfBuiltNames != nil {
		names, err := s.SelfBuiltNames.EndpointNames(ctx, tx)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if name == in.Name {
				conflicts = append(conflicts, NameConflict{Type: "self-built", Name: name})
			}
		}
	}
	if len(conflicts) > 0 {
		return nil, &NameConflictError{Conflicts: conflicts}
	}
	raw, err := s.Vault.Open(sealed)
	if err != nil {
		return nil, err
	}
	var p proxy.Proxy
	err = json.Unmarshal(raw, &p)
	clear(raw)
	if err != nil {
		return nil, err
	}
	p.Name = in.Name
	raw, err = json.Marshal(p)
	if err != nil {
		return nil, err
	}
	sealed, err = s.Vault.Seal(raw)
	clear(raw)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE imported_proxies SET name=?,payload_encrypted=?,revision=revision+1 WHERE id=?", in.Name, sealed, key); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return importedItem(key, src, in.Name, protocol, kind, revision+1, p), nil
}
