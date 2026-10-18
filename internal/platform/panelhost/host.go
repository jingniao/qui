// Package panelhost coordinates the single panel process across online restores.
package panelhost

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/system"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/recovery"
	"proxycontrol/internal/platform/restoretasks"
	"proxycontrol/internal/platform/server"
	"proxycontrol/internal/platform/vault"
	"sync"
)

type job struct {
	prepared archive.PreparedRestore
	password []byte
	record   restoretasks.Record
}
type receiver struct {
	mu       sync.Mutex
	busy     bool
	accepted bool
	queue    chan job
	store    restoretasks.Store
	database *sql.DB
	target   archive.RestoreTarget
}

func (r *receiver) hooks() *system.OnlineRestore {
	return &system.OnlineRestore{Target: r.target, Reserve: func() (func(), error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.busy {
			return nil, errors.New("restore busy")
		}
		r.busy = true
		return func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.accepted {
				r.busy = false
			}
		}, nil
	}, Accept: func(ctx context.Context, prepared archive.PreparedRestore, password []byte, actor, ip string) (restoretasks.Record, func(), error) {
		record, err := r.store.Begin(ctx, prepared.Plan.Source.ID, prepared.Plan.ArchiveSHA256)
		if err != nil {
			return record, nil, err
		}
		if err := (&system.Repository{Database: r.database}).Record(ctx, actor, "申请系统恢复", record.ID, "成功", ip); err != nil {
			r.store.Advance(context.Background(), record.ID, "preparing", "failed", "restore_failed")
			return record, nil, err
		}
		r.mu.Lock()
		r.accepted = true
		r.mu.Unlock()
		var once sync.Once
		return record, func() { once.Do(func() { r.queue <- job{prepared: prepared, password: password, record: record} }) }, nil
	}}
}

// Run holds no additional process and never releases panel lifetime ownership
// during stop, snapshot reconciliation, file replacement, or service rebuilding.
func Run(ctx context.Context, owner *panelstore.Ownership, c config.Panel, configurationPath string, files fs.FS, logger *slog.Logger) (resultErr error) {
	if err := owner.AssertHeld(c.Database, c.NodeIdentityDirectory); err != nil {
		return err
	}
	if logger == nil {
		logger = slog.Default()
	}
	database, err := db.Open(ctx, c.Database)
	if err != nil {
		return err
	}
	defer func() {
		if database != nil {
			resultErr = errors.Join(resultErr, database.Close())
		}
	}()
	httpListener, grpcListener, err := listeners(c)
	if err != nil {
		return err
	}
	defer func() { httpListener.Close(); grpcListener.Close() }()
	c.HTTPAddress = httpListener.Addr().String()
	c.GRPCAddress = grpcListener.Addr().String()
	app, err := server.New(c, database, files, logger)
	if err != nil {
		httpListener.Close()
		grpcListener.Close()
		return err
	}
	key, err := os.ReadFile(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		return err
	}
	defer clear(key)
	keyVault, err := vault.FromKey(key)
	if err != nil {
		return err
	}
	r := &receiver{queue: make(chan job, 1), store: restoretasks.Store{Directory: filepath.Join(filepath.Dir(c.Database), "restore-progress"), Key: key}, database: database}
	defer func() {
		for {
			select {
			case pending := <-r.queue:
				clear(pending.password)
				archive.DiscardPreparedRestore(context.Background(), pending.prepared)
				r.store.Advance(context.Background(), pending.record.ID, "preparing", "failed", "restore_failed")
			default:
				return
			}
		}
	}()
	if configurationPath != "" {
		absolute, err := filepath.Abs(configurationPath)
		if err != nil {
			return err
		}
		absolute, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return err
		}
		r.target = archive.RestoreTarget{Panel: c, ConfigurationPath: absolute}
		app.ConfigureRestore(r.hooks())
	}
	var runtime *server.PreparedRestore
	servedRecord := ""
	for {
		serviceCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		if runtime == nil {
			go func() { done <- app.Serve(serviceCtx, httpListener, grpcListener) }()
		} else {
			active := runtime
			go func() { done <- active.Serve(serviceCtx) }()
		}
		var request job
		select {
		case err := <-done:
			stop()
			if servedRecord != "" {
				err = errors.Join(err, r.store.FinishServing(ctx, servedRecord, err))
			}
			return err
		case <-ctx.Done():
			stop()
			serveErr := <-done
			if servedRecord != "" {
				serveErr = errors.Join(serveErr, r.store.FinishServing(ctx, servedRecord, serveErr))
			}
			return serveErr
		case request = <-r.queue:
			stop()
			if err := <-done; err != nil {
				clear(request.password)
				archive.DiscardPreparedRestore(context.Background(), request.prepared)
				return err
			}
		}
		if servedRecord != "" {
			if err := r.store.FinishServing(ctx, servedRecord, nil); err != nil {
				return err
			}
			servedRecord = ""
		}
		if err := ctx.Err(); err != nil {
			clear(request.password)
			archive.DiscardPreparedRestore(context.Background(), request.prepared)
			return nil
		}
		if database.PingContext(ctx) != nil {
			database, err = db.Open(ctx, c.Database)
			if err != nil {
				clear(request.password)
				return err
			}
		}
		source := archive.Source{Database: database, Config: c, ConfigurationVault: keyVault}
		next, restoreErr := execute(ctx, owner, source, request, files, logger, r.store)
		clear(request.password)
		if restoreErr != nil {
			logger.Error("online restore failed; checking recoverable current service", "restore_id", request.record.ID)
			if err := recovery.RecoverFiles(ctx, owner); err != nil {
				return errors.Join(restoreErr, err)
			}
			if database.PingContext(ctx) != nil {
				database, err = db.Open(ctx, c.Database)
				if err != nil {
					return errors.Join(restoreErr, err)
				}
			}
			app, err = server.New(c, database, files, logger)
			if err != nil {
				return errors.Join(restoreErr, err)
			}
			httpListener, grpcListener, err = listeners(c)
			if err != nil {
				return errors.Join(restoreErr, err)
			}
			runtime = nil
		} else {
			runtime = next
			database = runtime.Database()
			servedRecord = request.record.ID
		}
		r.database = database
		r.mu.Lock()
		r.busy = false
		r.accepted = false
		r.mu.Unlock()
		if configurationPath != "" {
			if runtime == nil {
				app.ConfigureRestore(r.hooks())
			} else {
				runtime.ConfigureRestore(r.hooks())
			}
		}
	}
}

func listeners(c config.Panel) (net.Listener, net.Listener, error) {
	httpListener, err := net.Listen("tcp", c.HTTPAddress)
	if err != nil {
		return nil, nil, err
	}
	grpcListener, err := net.Listen("tcp", c.GRPCAddress)
	if err != nil {
		httpListener.Close()
		return nil, nil, err
	}
	return httpListener, grpcListener, nil
}

func execute(ctx context.Context, owner *panelstore.Ownership, source archive.Source, request job, files fs.FS, logger *slog.Logger, store restoretasks.Store) (runtime *server.PreparedRestore, resultErr error) {
	prepared := request.prepared
	stage := "preparing"
	preparedRetained := true
	defer func() {
		if preparedRetained {
			resultErr = errors.Join(resultErr, archive.DiscardPreparedRestore(context.Background(), prepared))
		}
		if resultErr != nil {
			store.Advance(context.Background(), request.record.ID, stage, "failed", "restore_failed")
			if runtime != nil {
				runtime.Close()
				runtime = nil
			}
		}
	}()
	password := string(bytes.Clone(request.password))
	candidate, err := recovery.PrepareDatabase(ctx, owner, source, prepared, password)
	if err != nil {
		return nil, err
	}
	candidateRetained := true
	defer func() {
		if candidateRetained {
			resultErr = errors.Join(resultErr, recovery.DiscardCandidate(context.Background(), candidate, prepared))
		}
	}()
	if _, err := store.Advance(ctx, request.record.ID, "installing", "active", ""); err != nil {
		return nil, err
	}
	stage = "installing"
	runtime, err = recovery.InstallAndPrepare(ctx, owner, source, candidate, prepared, password, files, logger)
	if err != nil {
		return nil, err
	}
	if _, err := store.Advance(ctx, request.record.ID, "committed", "active", ""); err != nil {
		return runtime, err
	}
	stage = "committed"
	if _, err := recovery.FinalizeCandidate(ctx, candidate, prepared); err != nil {
		return runtime, err
	}
	candidateRetained = false
	if err := archive.DiscardPreparedRestore(ctx, prepared); err != nil {
		return runtime, err
	}
	preparedRetained = false
	if _, err := store.Advance(ctx, request.record.ID, "cleaned", "active", ""); err != nil {
		return runtime, err
	}
	stage = "cleaned"
	if _, err := store.Advance(ctx, request.record.ID, "serving", "active", ""); err != nil {
		return runtime, err
	}
	recordStartup, err := store.StartupRecorder(ctx, request.record.ID)
	if err != nil {
		return runtime, err
	}
	if err := runtime.ConfigureStartupRecorder(recordStartup); err != nil {
		return runtime, err
	}
	return runtime, nil
}
