// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Command server runs the reporting service: anonymous and named reports and
// the officer case work, over gRPC. It asks identity who is an officer and
// publishes steward-audit's AuditEvent through a transactional outbox.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bugs5382/go-buildinfo"
	"github.com/Bugs5382/go-buildinfo/health"
	log "github.com/Bugs5382/go-log"
	gootel "github.com/Bugs5382/go-otel"
	outbox "github.com/Bugs5382/go-outbox"
	outboxrabbitmq "github.com/Bugs5382/go-outbox/rabbitmq"
	postgres "github.com/Bugs5382/go-postgres"
	pgotel "github.com/Bugs5382/go-postgres/otel"
	"github.com/Bugs5382/go-rabbitmq"
	rmqotel "github.com/Bugs5382/go-rabbitmq/otel"
	"google.golang.org/grpc"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	identityv1 "github.com/Steward-GRC/steward-reporting/gen/go/thirdparty/identity/v1"
	"github.com/Steward-GRC/steward-reporting/internal/access"
	"github.com/Steward-GRC/steward-reporting/internal/audit"
	"github.com/Steward-GRC/steward-reporting/internal/config"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/grpcsvc"
	"github.com/Steward-GRC/steward-reporting/internal/readiness"
	"github.com/Steward-GRC/steward-reporting/internal/server"
	"github.com/Steward-GRC/steward-reporting/internal/store"
	"github.com/Steward-GRC/steward-reporting/internal/workloadauth"
)

const serviceName = "reporting"

// auditOutboxTable holds the audit events waiting for the relay.
const auditOutboxTable = "audit_outbox"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger := log.NewLogger(serviceName)
	if err := run(ctx, logger); err != nil {
		logger.Fatal(err, "reporting service stopped")
	}
}

func run(ctx context.Context, logger log.Logger) error {
	bi := buildinfo.Get()
	logger.Info("starting", log.F("version", bi.Version), log.F("commit", bi.Commit))
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	otelShutdown, err := gootel.Init(ctx, serviceName, cfg.OTLPEndpoint)
	if err != nil {
		return fmt.Errorf("otel: %w", err)
	}
	defer func() {
		if err := otelShutdown(context.Background()); err != nil {
			logger.Warn("otel shutdown", log.F("error", err.Error()))
		}
	}()

	if err := pgotel.InstrumentMigrate(ctx, serviceName, func() error {
		return postgres.Migrate(cfg.MigrateDSN, cfg.MigrationsDir)
	}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	db, err := postgres.New(ctx, cfg.DatabaseDSN, pgotel.WithTracing())
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer db.Close()

	conn, err := rabbitmq.Connect(ctx, cfg.RabbitURL, append(rmqotel.Instrument(), rabbitmq.WithLogger(rabbitLogger{logger}))...)
	if err != nil {
		return fmt.Errorf("rabbitmq: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Every audit event is written to the outbox in the transaction of the
	// view or change it records, and the relay ships it to the audit
	// exchange after the commit.
	ob, err := outbox.New(outbox.WithTable(auditOutboxTable), outbox.WithLogger(logger))
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	if err := ob.Migrate(ctx, db); err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	relay := ob.NewRelay(db, outboxrabbitmq.New(conn, audit.Exchange,
		rabbitmq.WithExchangeDeclare(rabbitmq.ExchangeConfig{Name: audit.Exchange, Kind: "topic", Durable: true})))
	go func() {
		if err := relay.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error(err, "audit outbox relay stopped")
		}
	}()
	auditor := audit.New(store.NewOutboxPublisher(db, ob, audit.ContentType))

	dialOpts, err := server.DialOptions(cfg.TokenFile)
	if err != nil {
		return fmt.Errorf("workload token: %w", err)
	}
	identityConn, err := grpc.NewClient(cfg.IdentityAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("identity dial: %w", err)
	}
	defer func() { _ = identityConn.Close() }()
	st := store.New(db)
	seeded, err := st.SeedSettings(ctx, domain.DefaultSettings(cfg.OfficerGroups))
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if seeded {
		logger.Info("compliance settings seeded", log.F("officer_groups", len(cfg.OfficerGroups)))
		if len(cfg.OfficerGroups) == 0 {
			logger.Warn("REPORTING_OFFICER_GROUPS is not set: nobody can open cases until a compliance admin names the officer groups")
		}
	} else {
		logger.Info("compliance settings already stored; REPORTING_OFFICER_GROUPS is ignored")
	}
	deps := grpcsvc.Deps{
		DB: db, Store: st, Audit: auditor,
		Officers:   access.NewFromSource(access.IdentityUsers(identityv1.NewIdentityReadServiceClient(identityConn)), grpcsvc.OfficerGroups(st)),
		NoticeDays: cfg.NoticeDays, Log: logger,
	}

	readyDeps := readiness.Deps{Postgres: readiness.PostgresDB(db), Broker: conn, Identity: readiness.GRPCPeer(identityConn)}
	serveOpts := server.Options{Policy: grpcsvc.CallerPolicy, OnDeny: auditDenial(auditor, logger)}
	if cfg.WorkloadAuth {
		verifier, err := workloadauth.NewVerifier(cfg.Workload, logger)
		if err != nil {
			return fmt.Errorf("workloadauth: %w", err)
		}
		go verifier.Run(ctx)
		serveOpts.Verifier = verifier
		readyDeps.WorkloadKeys = verifier.Refresh
	} else {
		go workloadauth.WarnDisabled(ctx, logger, workloadauth.DisabledWarnInterval)
		readyDeps.WorkloadAuthDisabled = true
	}
	checker, err := readiness.New(readyDeps, health.WithTTL(5*time.Second), health.WithTimeout(2*time.Second), health.WithLogger(logger))
	if err != nil {
		return fmt.Errorf("readiness: %w", err)
	}
	serveOpts.Checker = checker

	var lc net.ListenConfig
	grpcLis, err := lc.Listen(ctx, "tcp", ":"+cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	probeLis, err := lc.Listen(ctx, "tcp", ":"+cfg.ProbePort)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	logger.Info("serving", log.F("grpc_port", cfg.GRPCPort), log.F("probe_port", cfg.ProbePort))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	probesDone := make(chan error, 1)
	go func() {
		probesDone <- server.ServeProbes(ctx, probeLis, checker)
		cancel()
	}()
	err = server.Serve(ctx, grpcLis, logger, serveOpts, func(s *grpc.Server) {
		reportingv1.RegisterIntakeServiceServer(s, grpcsvc.NewIntake(deps))
		reportingv1.RegisterCaseServiceServer(s, grpcsvc.NewCases(deps))
		reportingv1.RegisterSettingsServiceServer(s, grpcsvc.NewSettings(deps))
	})
	cancel()
	return errors.Join(err, <-probesDone)
}

// auditDenial audits every call workloadauth refuses. The request is never
// recorded, only who called what and why it was refused.
func auditDenial(auditor *audit.Emitter, logger log.Logger) workloadauth.DenyHook {
	return func(ctx context.Context, d workloadauth.Denial) {
		if err := auditor.Emit(ctx, audit.Event{
			Tier:    audit.TierAudit,
			Action:  "reporting.call.refused",
			Subject: "method:" + d.Method,
			Attributes: map[string]string{
				"caller":          d.Caller.Name,
				"service_account": d.Caller.ServiceAccount,
				"code":            d.Code.String(),
				"reason":          d.Reason,
			},
		}); err != nil {
			logger.Ctx(ctx).Warn("emit reporting.call.refused", log.F("error", err.Error()))
		}
	}
}

type rabbitLogger struct{ l log.Logger }

func (r rabbitLogger) Debugf(f string, a ...any) { r.l.Debug(fmt.Sprintf(f, a...)) }
func (r rabbitLogger) Infof(f string, a ...any)  { r.l.Info(fmt.Sprintf(f, a...)) }
func (r rabbitLogger) Warnf(f string, a ...any)  { r.l.Warn(fmt.Sprintf(f, a...)) }
func (r rabbitLogger) Errorf(f string, a ...any) { r.l.Error(nil, fmt.Sprintf(f, a...)) }
