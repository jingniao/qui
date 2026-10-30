package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/connections"
	"proxycontrol/internal/panel/dashboard"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/policy"
	"proxycontrol/internal/panel/portal"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/system"
	backupArchive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/events"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

type Server struct {
	restoreStartup     func(context.Context) error
	Config             config.Panel
	Database           *sql.DB
	Logger             *slog.Logger
	HTTP               *http.Server
	httpMux            *http.ServeMux
	GRPC               *grpc.Server
	health             *health.Server
	Authority          *pki.Authority
	accessRepository   *access.Repository
	trafficService     *access.Service
	nodeRepository     *node.Repository
	deployments        *deployment.Repository
	readPool           *sql.DB
	retentionWake      chan struct{}
	backupWake         chan struct{}
	backupService      *system.BackupService
	preparedTLS        bool
	systemHandler      *system.Handler
	authService        *auth.Service
	quotaDeployer      *quotaDeployer
	latencyScheduler   *node.LatencyScheduler
	cycleScheduler     *cycleScheduler
	importStore        *importer.Store
	connectionStore    *connections.Store
	connectionObserver *user.ConnectionObserver
	deviceObserver     *user.DeviceObserver
	admission          admission
	startedAt          time.Time
}

func New(c config.Panel, database *sql.DB, files fs.FS, logger *slog.Logger) (*Server, error) {
	return newServer(c, database, files, logger, false)
}

func newServer(c config.Panel, database *sql.DB, files fs.FS, logger *slog.Logger, existing bool) (app *Server, resultErr error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{Config: c, Database: database, Logger: logger, health: health.NewServer(), retentionWake: make(chan struct{}, 1), startedAt: time.Now().UTC()}
	// The read-only pool keeps login, subscriptions and lists responsive while
	// the single writer connection is busy. A pool that cannot be opened only
	// costs that isolation, so it never fails startup.
	readPool, err := db.OpenReadPool(context.Background(), c.Database, db.ReadPoolConnections)
	if err != nil {
		logger.Warn("read-only database pool unavailable; using the writer connection for reads", "error", err)
	} else {
		s.readPool = readPool
	}
	// A construction that fails after this point must not leak the pool.
	defer func() {
		if resultErr != nil {
			s.closeReadPool()
		}
	}()
	s.backupWake = make(chan struct{}, 1)
	loadAuthority := pki.LoadOrCreate
	if existing {
		loadAuthority = pki.LoadExisting
	}
	authority, err := loadAuthority(c.NodeIdentityDirectory)
	if err != nil {
		return nil, err
	}
	s.Authority = authority
	nodeRepository := &node.Repository{Database: database}
	var configurationVault *vault.Vault
	if existing {
		key, readErr := os.ReadFile(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
		if readErr != nil {
			return nil, readErr
		}
		configurationVault, err = vault.FromKey(key)
		clear(key)
	} else {
		configurationVault, err = vault.Open(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
	}
	if err != nil {
		return nil, err
	}
	users := &user.Repository{Database: database, Vault: configurationVault}
	deployments := &deployment.Repository{Database: database, Vault: configurationVault}
	s.nodeRepository = nodeRepository
	s.deployments = deployments
	s.accessRepository = &access.Repository{Database: database, Vault: configurationVault}
	eventBus := &events.Bus{}
	notify := func(id string) { eventBus.Publish("node_status", map[string]string{"nodeId": id}) }
	control := &node.Control{Repository: nodeRepository, Authority: authority, Notify: notify, Deployment: deployments, DeploymentNotify: func(revision int64) { eventBus.Publish("deployment_status", map[string]int64{"revision": revision}) }}
	control.DashboardNotify = func() { eventBus.Publish("dashboard_status", map[string]bool{"changed": true}) }
	s.connectionStore = connections.NewStore(3 * node.HeartbeatInterval)
	s.connectionObserver = &user.ConnectionObserver{Repository: users, Nodes: nodeRepository, Store: s.connectionStore, Publish: eventBus.Publish}
	control.Connections = s.connectionStore
	devices := &user.DeviceObserver{Repository: users, Nodes: nodeRepository, States: deployments, Store: s.connectionStore, Admission: &connections.Admission{}, Publish: eventBus.Publish}
	s.deviceObserver = devices
	control.DeviceGates = devices.Command
	control.GateChanges = devices.Subscribe
	control.ConnectionsRecorded = func(ctx context.Context, id string) { s.connectionObserver.Evaluate(ctx, id); devices.Record(ctx, id) }
	s.trafficService = &access.Service{Repository: s.accessRepository, Snapshots: deployments, Users: users}
	control.AccessRecorder = s.trafficService
	options := []grpc.ServerOption{grpc.MaxRecvMsgSize(4 << 20), grpc.MaxSendMsgSize(4 << 20), grpc.MaxConcurrentStreams(32), grpc.UnaryInterceptor(node.UnaryAuthentication), grpc.StreamInterceptor(node.StreamAuthentication), grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second}), grpc.KeepaliveParams(keepalive.ServerParameters{Time: 30 * time.Second, Timeout: 10 * time.Second})}
	if !c.Development {
		tlsConfig, err := c.TLS.ServerConfig()
		if err != nil {
			return nil, err
		}
		options = append(options, grpc.Creds(credentials.NewTLS(node.EnrollmentTLS(tlsConfig, authority.VerificationPEM()))))
	} else {
		loadServer := authority.DevelopmentServer
		if existing {
			loadServer = authority.ExistingDevelopmentServer
		}
		certificate, err := loadServer()
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		roots.AppendCertsFromPEM(authority.VerificationPEM())
		options = append(options, grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: roots, ClientAuth: tls.VerifyClientCertIfGiven})))
	}
	s.GRPC = grpc.NewServer(options...)
	control.CoreUpdates = deployments
	control.AgentUpdates = deployments
	pb.RegisterAgentControlServer(s.GRPC, control)
	healthpb.RegisterHealthServer(s.GRPC, s.health)
	mux := http.NewServeMux()
	s.httpMux = mux
	systemRepository := &system.Repository{Database: database, Reader: s.readPool}
	// One format registry per process: the subscription handler renders through it,
	// the runtime policy validates the stored default against it, and it must be in
	// place before the startup read below.
	subscriptionFormats := subscription.DefaultFormats()
	systemRepository.SubscriptionFormats = subscriptionFormats.Known
	if _, err := systemRepository.ReadRuntimeSettings(context.Background()); err != nil {
		return nil, err
	}
	var releaseKeys map[string]release.TrustedKey
	if len(c.ReleaseKeys) > 0 {
		releaseKeys, err = release.PinnedKeys(c.ReleaseKeys)
		if err != nil {
			return nil, err
		}
	}
	nodeRepository.CertificateAudit = systemRepository
	s.backupService = &system.BackupService{Repository: systemRepository, Source: backupArchive.Source{Database: database, Config: c, ConfigurationVault: configurationVault}, Directory: filepath.Join(filepath.Dir(c.Database), "backups"), Notify: func() { eventBus.Publish("backup_status", map[string]bool{"changed": true}) }}
	s.accessRepository.Settings = systemRepository
	s.accessRepository.StorageThresholds = systemRepository
	authService := &auth.Service{Repository: &auth.Repository{Database: database, Reader: s.readPool}, Audit: systemRepository, Settings: systemRepository, SessionDuration: 2 * time.Hour}
	s.authService = authService
	// Warm the cached first-run and base-setting state, so requests never pay
	// for the lookup that prepareSetupToken has already done.
	if err := authService.RefreshSetupState(context.Background()); err != nil {
		logger.Warn("cached setup state unavailable at startup", "error", err)
	}
	if err := s.prepareSetupToken(); err != nil {
		return nil, err
	}
	trustedProxies, err := c.Server.ParsedTrustedProxies()
	if err != nil {
		return nil, err
	}
	authHandler := &auth.Handler{Service: authService, Secure: !c.Development, Logger: logger, TrustedProxies: trustedProxies}
	authHandler.Register(mux)
	(&system.ReleaseHandler{Repository: systemRepository, Auth: authHandler, Keys: releaseKeys, ActorName: func(ctx context.Context) string { return auth.Actor(ctx).Username }}).Register(mux)
	(&deployment.CoreUpdateHandler{Repository: deployments, Audit: systemRepository, Auth: authHandler}).Register(mux)
	(&deployment.AgentUpdateHandler{Repository: deployments, Catalogue: systemRepository, Auth: authHandler, Keys: releaseKeys}).Register(mux)
	s.systemHandler = &system.Handler{Repository: systemRepository, Backups: s.backupService, BackupChanged: func() {
		eventBus.Publish("backup_status", map[string]bool{"changed": true})
		select {
		case s.backupWake <- struct{}{}:
		default:
		}
	}, Auth: authHandler, ActorName: func(ctx context.Context) string { return auth.Actor(ctx).Username }, ActorID: func(ctx context.Context) string { return auth.Actor(ctx).ID }, Logger: logger, BaseChanged: func() {
		eventBus.Publish("system_settings", map[string]bool{"changed": true})
		eventBus.Publish("dashboard_status", map[string]bool{"changed": true})
		// The platform name and timezone are part of every session answer.
		if err := authService.RefreshSetupState(context.Background()); err != nil {
			authService.InvalidateSetupState()
		}
	}, RetentionChanged: func() {
		select {
		case s.retentionWake <- struct{}{}:
		default:
		}
	}}
	s.systemHandler.Runtime = s.runtimeInfo
	s.systemHandler.Register(mux)
	(&access.Handler{Repository: s.accessRepository, Auth: authHandler, Service: s.trafficService, Audit: systemRepository, BaseSettings: systemRepository}).Register(mux)
	nodeHandler := &node.Handler{Repository: nodeRepository, Audit: systemRepository, Auth: authHandler, Notify: notify, Authority: authority, Tasks: deployments}
	nodeHandler.Install = s.installInfo
	nodeHandler.Register(mux)
	nodeHandler.RegisterGroups(mux)
	proxyService := &proxy.Service{Repository: &proxy.Repository{Database: database, Vault: configurationVault}, Nodes: nodeRepository}
	proxyService.Deploy = &deployment.CanvasPublisher{Repository: deployments}
	policyService := &policy.Service{Repository: &policy.Repository{Database: database}, Nodes: nodeRepository, Users: users, Proxies: proxyService.Repository, Settings: systemRepository}
	proxyService.Policies = policyService
	(&dashboard.Handler{Service: &dashboard.Service{Repository: &dashboard.Repository{Database: database, Connections: s.connectionStore}, Nodes: nodeRepository, Deployments: deployments, Proxies: proxyService.Repository, Settings: systemRepository}, Auth: authHandler}).Register(mux)
	control.HealthRecorder = proxyService.Repository
	(&policy.Handler{Service: policyService, Auth: authHandler, Audit: systemRepository}).Register(mux)
	proxyHandler := &proxy.Handler{Service: proxyService, Auth: authHandler, Audit: systemRepository, Users: users}
	proxyHandler.Register(mux)
	proxyHandler.RegisterGroups(mux)
	// Every user has exactly one access identity with one credential per allowed
	// protocol (plan 4.1). An upgrade (migration 0113), a restored older backup or
	// a protocol list edited elsewhere is brought in line here, before the quota
	// deployer's startup reconciliation publishes the nodes whose user access
	// surface now differs.
	if changed, err := users.EnsureAllAccessIdentities(context.Background()); err != nil {
		return nil, err
	} else if len(changed) > 0 {
		logger.Info("access identities updated at startup", "users", len(changed))
	}
	quotaState := user.NewQuotaState(users)
	if err := quotaState.Seed(context.Background()); err != nil {
		return nil, err
	}
	s.quotaDeployer = newQuotaDeployer(logger, users, quotaState, proxyService, nodeRepository, deployments, eventBus.Publish)
	s.quotaDeployer.record = systemRepository.Record
	s.latencyScheduler = &node.LatencyScheduler{Repository: nodeRepository, Logger: logger}
	s.cycleScheduler = &cycleScheduler{users: users, nodes: nodeRepository, settings: systemRepository, quota: s.quotaDeployer, now: time.Now, publish: eventBus.Publish}
	users.TrafficApplied = s.quotaDeployer.evaluate
	// A deploy or rollback re-publishes a stored revision verbatim, so the node's
	// user access surface is only known to be correct after the Deployment module
	// reports it applied.
	deployments.Applied = s.quotaDeployer.reconcileNode
	userHandler := &user.Handler{Repository: users, Auth: authHandler, Audit: systemRepository, Nodes: nodeRepository, QuotaChanged: s.quotaDeployer.evaluateUser, ExpiryRenewed: s.quotaDeployer.renewExpiry, AccessRemoved: s.quotaDeployer.accessRemoved, AccessChanged: s.quotaDeployer.accessChanged, Observations: s.connectionObserver, Devices: devices}
	userHandler.Register(mux)
	// The subscription path only reads users, nodes and published state, and a
	// snapshot that lags a just-committed write is acceptable there. Pointing
	// those reads at the read-only pool keeps /sub responsive while the single
	// writer connection is busy.
	subscriptionReads := database
	if s.readPool != nil {
		subscriptionReads = s.readPool
	}
	subscriptionUsers := *users
	subscriptionUsers.Database = subscriptionReads
	subscriptionNodes := *nodeRepository
	subscriptionNodes.Database = subscriptionReads
	subscriptionPublished := *deployments
	subscriptionPublished.Database = subscriptionReads
	subscriptionService := &subscription.Service{
		Repository: &subscription.Repository{Database: database, Reader: s.readPool, Vault: configurationVault},
		Users:      &subscriptionUsers,
		Sources:    source.NewRegistry(&published.Source{Users: &subscriptionUsers, Nodes: &subscriptionNodes, Published: &subscriptionPublished}),
		Formats:    subscriptionFormats,
		Settings:   systemRepository,
		Traffic:    s.accessRepository,
		Rules:      &rules.Store{Database: subscriptionReads},
	}
	s.importStore = &importer.Store{Database: database, Reader: s.readPool, Vault: configurationVault}
	subscriptionService.Sources.Register(s.importStore)
	subscriptionService.ClientChains = s.importStore
	(&importer.Handler{Store: s.importStore, Auth: authHandler, Audit: systemRepository}).Register(mux)
	portalRepository := &portal.Repository{Database: database}
	(&portal.Handler{Repository: portalRepository, Auth: authHandler, Audit: systemRepository,
		Service: &portal.Service{Repository: portalRepository, Auth: authService, Users: users, Nodes: nodeRepository, Traffic: s.accessRepository, Subscriptions: subscriptionService, Devices: devices},
	}).Register(mux)
	// The client rule template is written through the writer connection and
	// checked against the node and policy modules, which own what it refers to.
	ruleStore := &rules.Store{Database: database, Lookup: ruleLookup{nodes: nodeRepository, policies: policyService.Repository}}
	(&subscription.Handler{Service: subscriptionService, Auth: authHandler, Audit: systemRepository, Rules: ruleStore, ServiceRules: serviceRules{policies: policyService.Repository}}).Register(mux)
	(&deployment.Handler{Repository: deployments, Nodes: nodeRepository, Auth: authHandler, Audit: systemRepository, Users: users, Proxy: proxyService}).Register(mux)
	mux.Handle("GET /api/v1/events", eventBus.Handler(authHandler))
	mux.Handle("GET /api/v1/operations", authHandler.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, size, err := httpapi.Pagination(r)
		if err != nil {
			httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
			return
		}
		items, total, err := systemRepository.List(r.Context(), r.URL.Query().Get("keyword"), page, size)
		if err != nil {
			logger.Error("operation query failed", "error", err)
			httpapi.Error(w, 500, "internal_error", "Operation query failed")
			return
		}
		httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
	}), true))
	mux.HandleFunc("GET /healthz", s.healthHTTP)
	mux.Handle("/api/", authHandler.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(w, 404, "not_found", "API endpoint not found")
	}), true))
	mux.HandleFunc("/sub/", func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(w, 404, "not_found", "Subscription not found")
	})
	mux.HandleFunc("GET /install/ca.pem", s.installCA)
	mux.Handle("/", staticHandler(files))
	s.HTTP = &http.Server{Addr: c.HTTPAddress, Handler: s.admission.wrap(securityHeaders(mux, !c.Development)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	if !c.Development {
		s.HTTP.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13}
		if existing {
			material, err := c.TLS.ServerConfig()
			if err != nil {
				return nil, err
			}
			s.HTTP.TLSConfig.Certificates = material.Certificates
			s.preparedTLS = true
		}
	}
	return s, nil
}

// SetupToken returns the one-time first-run setup token, or "" once an Owner
// exists or setup has completed. It exists so tests and deployment tooling can
// present the token without weakening the HTTP path.
func (s *Server) SetupToken() string {
	if s.authService == nil {
		return ""
	}
	return s.authService.CurrentSetupToken()
}

// prepareSetupToken generates and publishes the one-time first-run token when
// the panel has no Owner yet. The token is logged once and written to
// <data_dir>/setup-token with mode 0600; the auth service removes the file and
// clears the token after a successful setup.
func (s *Server) prepareSetupToken() error {
	initialized, err := s.authService.Repository.Initialized(context.Background())
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenPath := filepath.Join(filepath.Dir(s.Config.Database), "setup-token")
	// os.WriteFile keeps the mode of an already-existing file, so a stale token
	// file with wider permissions would stay wider. Remove it first, then create
	// exclusively with mode 0600.
	if err := os.Remove(tokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale setup token: %w", err)
	}
	file, err := os.OpenFile(tokenPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("write setup token: %w", err)
	}
	if _, err := file.WriteString(token + "\n"); err != nil {
		file.Close()
		return fmt.Errorf("write setup token: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write setup token: %w", err)
	}
	s.authService.SetSetupToken(token, tokenPath)
	s.Logger.Warn("first-run setup token", "token", token, "file", tokenPath)
	return nil
}

func (s *Server) healthHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Database.PingContext(ctx); err != nil {
		httpapi.Error(w, 503, "not_ready", "Database unavailable")
		return
	}
	httpapi.JSON(w, 200, map[string]string{"status": "ok", "service": "proxy-panel"})
}

func (s *Server) closeReadPool() {
	if s.readPool == nil {
		return
	}
	pool := s.readPool
	s.readPool = nil
	pool.Close()
}

func (s *Server) Serve(ctx context.Context, httpListener, grpcListener net.Listener) error {
	defer s.closeReadPool()
	if s.restoreStartup != nil {
		if err := s.restoreStartup(ctx); err != nil {
			return err
		}
	}
	background, stopBackground := context.WithCancel(ctx)
	var jobs sync.WaitGroup
	jobs.Add(7)
	defer func() { stopBackground(); jobs.Wait() }()
	go func() { defer jobs.Done(); s.cycleScheduler.run(background) }()
	go func() { defer jobs.Done(); s.latencyScheduler.Run(background) }()
	go func() { defer jobs.Done(); s.importStore.Run(background) }()
	go func() {
		defer jobs.Done()
		s.quotaDeployer.run(background)
	}()
	go func() {
		defer jobs.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			operation, cancel := context.WithTimeout(background, 5*time.Minute)
			err := s.backupService.RunDue(operation, time.Now().UTC())
			cancel()
			if err != nil && !errors.Is(err, system.ErrBackupBusy) && background.Err() == nil {
				s.Logger.Error("automatic backup operation failed")
			}
			select {
			case <-background.Done():
				return
			case <-ticker.C:
			case <-s.backupWake:
			}
		}
	}()
	go func() {
		defer jobs.Done()
		for {
			cleanup, cancel := context.WithTimeout(background, 10*time.Second)
			pending, err := s.accessRepository.AdvanceRetention(cleanup, time.Now().UTC())
			cancel()
			if err != nil && background.Err() == nil {
				s.Logger.Error("access retention cleanup failed")
			}
			var spaceErr error
			if err == nil && !pending && background.Err() == nil {
				begin, beginCancel := context.WithTimeout(background, 2*time.Second)
				attempt, beginErr := s.accessRepository.BeginSpaceMaintenance(begin)
				beginCancel()
				spaceErr = beginErr
				if beginErr == nil {
					maintenance, maintenanceCancel := context.WithTimeout(background, 2*time.Second)
					mode, reclaimErr := db.ReclaimFreePages(maintenance, s.Database)
					maintenanceCancel()
					// Persist a short attempt's result even during shutdown. If storage
					// remains unavailable, the durable running marker stays unconfirmed.
					record, recordCancel := context.WithTimeout(context.Background(), 2*time.Second)
					recordErr := s.accessRepository.FinishSpaceMaintenance(record, attempt, mode, reclaimErr != nil)
					recordCancel()
					spaceErr = errors.Join(reclaimErr, recordErr)
				}
				if spaceErr != nil && background.Err() == nil {
					s.Logger.Error("SQLite free page reclamation failed; committed retention progress is preserved")
				}
			}
			delay := time.Hour
			if pending {
				delay = time.Second
			}
			if err != nil || spaceErr != nil {
				delay = time.Minute
			}
			timer := time.NewTimer(delay)
			select {
			case <-background.Done():
				timer.Stop()
				return
			case <-timer.C:
			case <-s.retentionWake:
				timer.Stop()
			}
		}
	}()
	go func() {
		defer jobs.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			work, cancel := context.WithTimeout(background, 10*time.Second)
			err := s.trafficService.ResumePendingTraffic(work, time.Now().UTC())
			cancel()
			if err != nil && background.Err() == nil {
				s.Logger.Warn("traffic accounting continuation pending")
			}
			select {
			case <-background.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	s.HTTP.BaseContext = func(net.Listener) context.Context { return ctx }
	defer httpListener.Close()
	defer grpcListener.Close()
	errs := make(chan error, 2)
	s.health.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	go func() {
		var err error
		if s.Config.Development {
			err = s.HTTP.Serve(httpListener)
		} else {
			if s.preparedTLS {
				err = s.HTTP.ServeTLS(httpListener, "", "")
			} else {
				err = s.HTTP.ServeTLS(httpListener, s.Config.TLS.Certificate, s.Config.TLS.Key)
			}
		}
		if !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	go func() {
		if err := s.GRPC.Serve(grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errs <- err
		}
	}()
	s.Logger.Info("panel started", "http", httpListener.Addr().String(), "grpc", grpcListener.Addr().String(), "development", s.Config.Development)
	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-errs:
	}
	s.health.Shutdown()
	s.admission.stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.Config.ShutdownTimeout)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := s.HTTP.Shutdown(shutdownCtx); err != nil {
			s.HTTP.Close()
		}
	}()
	go func() {
		defer wg.Done()
		finished := make(chan struct{})
		go func() { s.GRPC.GracefulStop(); close(finished) }()
		select {
		case <-finished:
		case <-shutdownCtx.Done():
			s.GRPC.Stop()
			<-finished
		}
	}()
	wg.Wait()
	s.admission.wait()
	s.Logger.Info("panel stopped")
	return runErr
}

// ConfigureRestore is called only before serving; runtime replacement gets a
// fresh handler graph. No remote executable or arbitrary target is accepted.
func (s *Server) ConfigureRestore(hooks *system.OnlineRestore) { s.systemHandler.Restore = hooks }

func staticHandler(files fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpapi.Error(w, 405, "method_not_allowed", "Method not allowed")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if strings.HasPrefix(name, ".") {
			http.NotFound(w, r)
			return
		}
		stat, err := fs.Stat(files, name)
		if err == nil && !stat.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(path.Base(name), ".") || strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		index, err := fs.ReadFile(files, "index.html")
		if err != nil {
			httpapi.Error(w, 503, "ui_unavailable", "Build the frontend before building the panel binary")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method == http.MethodGet {
			w.Write(index)
		}
	})
}

func securityHeaders(next http.Handler, https bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'")
		if https {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
