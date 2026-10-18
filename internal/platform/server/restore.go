package server

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/panelstore"
	"sync"
	"time"
)

// PreparedRestore holds listening sockets without serving or running workers.
// It gates every database connection with query_only until the journal commits.
type PreparedRestore struct {
	config                     config.Panel
	files                      fs.FS
	logger                     *slog.Logger
	readonly                   *db.PreparedDatabase
	app                        *Server
	httpListener, grpcListener net.Listener
	mu                         sync.Mutex
	consumed                   bool
	ownership                  *panelstore.Ownership
}

func PrepareRestore(ctx context.Context, c config.Panel, files fs.FS, logger *slog.Logger) (prepared *PreparedRestore, resultErr error) {
	p := &PreparedRestore{config: c, files: files, logger: logger}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, p.Close())
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	database, err := db.OpenPrepared(ctx, c.Database)
	if err != nil {
		return nil, err
	}
	p.readonly = database
	p.app, err = newServer(c, database.Database, files, logger, true)
	if err != nil {
		return nil, err
	}
	var owners int
	if err := database.Database.QueryRowContext(ctx, "SELECT count(*) FROM admins WHERE role='Owner' AND status='active'").Scan(&owners); err != nil || owners != 1 {
		return nil, errors.New("restored service requires one active Owner")
	}
	settings := &system.Repository{Database: database.Database}
	if _, err := settings.ReadBase(ctx); err != nil {
		return nil, err
	}
	if _, err := settings.ReadRetention(ctx); err != nil {
		return nil, err
	}
	if _, err := settings.BackupSchedule(ctx); err != nil {
		return nil, err
	}
	if !c.Development {
		tlsConfig, err := c.TLS.ServerConfig()
		if err != nil {
			return nil, err
		}
		leaf, err := x509.ParseCertificate(tlsConfig.Certificates[0].Certificate[0])
		if err != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
			return nil, errors.New("restored TLS certificate is not currently valid")
		}
	}
	p.httpListener, err = net.Listen("tcp", c.HTTPAddress)
	if err != nil {
		return nil, err
	}
	p.grpcListener, err = net.Listen("tcp", c.GRPCAddress)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PreparedRestore) Addresses() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed || p.httpListener == nil || p.grpcListener == nil {
		return "", ""
	}
	return p.httpListener.Addr().String(), p.grpcListener.Addr().String()
}

// The same original ownership handle must remain held throughout preparation
// and serving. Binding cannot replace it with another directory or instance.
func (p *PreparedRestore) BindOwnership(owner *panelstore.Ownership) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed || p.readonly == nil || p.httpListener == nil || p.grpcListener == nil {
		return errors.New("restored preparation is not available")
	}
	if err := owner.AssertHeld(p.config.Database, p.config.NodeIdentityDirectory); err != nil {
		return err
	}
	if p.ownership != nil && p.ownership != owner {
		return errors.New("restored ownership already bound")
	}
	p.ownership = owner
	return nil
}
func (p *PreparedRestore) ConfigureStartupRecorder(record func(context.Context) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed || p.app == nil || p.ownership == nil || record == nil || p.app.restoreStartup != nil {
		return errors.New("restore startup recorder is not available")
	}
	p.app.restoreStartup = record
	return nil
}

func (p *PreparedRestore) ConfigureRestore(hooks *system.OnlineRestore) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.consumed {
		p.app.ConfigureRestore(hooks)
	}
}

func (p *PreparedRestore) Database() *sql.DB {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed {
		return nil
	}
	return p.readonly.Database
}

// Serve starts only after the coordinator has committed the file journal.
// The caller must retain panelstore ownership until Serve and Close finish.
func (p *PreparedRestore) Serve(ctx context.Context) error {
	p.mu.Lock()
	if p.consumed {
		p.mu.Unlock()
		return errors.New("restored listener reservation already consumed")
	}
	if p.ownership == nil {
		p.mu.Unlock()
		return errors.New("restored service requires original directory ownership")
	}
	if p.app == nil || p.app.restoreStartup == nil {
		p.mu.Unlock()
		return errors.New("restored service requires original startup result recorder")
	}
	if err := p.ownership.AssertHeld(p.config.Database, p.config.NodeIdentityDirectory); err != nil {
		p.mu.Unlock()
		return err
	}
	p.consumed = true
	readonly := p.readonly
	app := p.app
	httpListener, grpcListener := p.httpListener, p.grpcListener
	p.readonly = nil
	p.httpListener = nil
	p.grpcListener = nil
	p.mu.Unlock()
	defer httpListener.Close()
	defer grpcListener.Close()
	defer readonly.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.ownership.AssertHeld(p.config.Database, p.config.NodeIdentityDirectory); err != nil {
		return err
	}
	if err := readonly.EnableWrites(ctx); err != nil {
		return err
	}
	if err := p.ownership.AssertHeld(p.config.Database, p.config.NodeIdentityDirectory); err != nil {
		return err
	}
	return app.Serve(ctx, httpListener, grpcListener)
}

func (p *PreparedRestore) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.consumed {
		return nil
	}
	p.consumed = true
	var err error
	if p.readonly != nil {
		err = errors.Join(err, p.readonly.Close())
	}
	if p.httpListener != nil {
		err = errors.Join(err, p.httpListener.Close())
	}
	if p.grpcListener != nil {
		err = errors.Join(err, p.grpcListener.Close())
	}
	return err
}
