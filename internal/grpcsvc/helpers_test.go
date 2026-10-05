// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bugs5382/go-apperr/apperrgrpc"
	grpcactor "github.com/Bugs5382/go-grpc-actor"
	log "github.com/Bugs5382/go-log"
	outbox "github.com/Bugs5382/go-outbox"
	postgres "github.com/Bugs5382/go-postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/access"
	"github.com/Steward-GRC/steward-reporting/internal/audit"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/grpcsvc"
	"github.com/Steward-GRC/steward-reporting/internal/server"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

// The brief's sample people. Alice reports; Grace and Heidi are privacy
// officers; Carol is a compliance admin, Dave a site admin, Bob a reader.
const (
	alice = "0b9f3d0c-1a2b-4c3d-8e4f-00000000000a"
	bob   = "0b9f3d0c-1a2b-4c3d-8e4f-00000000000b"
	carol = "0b9f3d0c-1a2b-4c3d-8e4f-00000000000c"
	dave  = "0b9f3d0c-1a2b-4c3d-8e4f-00000000000d"
	grace = "0b9f3d0c-1a2b-4c3d-8e4f-000000000001"
	heidi = "0b9f3d0c-1a2b-4c3d-8e4f-000000000002"
	root  = "0b9f3d0c-1a2b-4c3d-8e4f-0000000000ff"

	officerGroup = "Privacy Officers"
	passphrase   = "correct horse battery"
	auditTable   = "audit_outbox"
)

// What identity knows about Alice. Reporting never learns it.
const (
	aliceName  = "Alice Example"
	aliceEmail = "alice@example.org"
)

type directory struct {
	users map[string]access.User
	calls atomic.Int64
}

func (d *directory) GetUser(_ context.Context, id string) (access.User, error) {
	d.calls.Add(1)
	u, ok := d.users[id]
	if !ok {
		return access.User{}, access.ErrNoUser
	}
	return u, nil
}

func newDirectory() *directory {
	return &directory{users: map[string]access.User{
		alice: {ID: alice, Enabled: true},
		bob:   {ID: bob, Enabled: true},
		carol: {ID: carol, Enabled: true, Roles: []string{"compliance-admin"}},
		dave:  {ID: dave, Enabled: true, Roles: []string{"site-admin"}},
		root:  {ID: root, Enabled: true, Root: true, Roles: []string{"site-admin"}},
		grace: {ID: grace, Enabled: true, IdpGroups: []string{officerGroup}},
		heidi: {ID: heidi, Enabled: true, IdpGroups: []string{officerGroup}},
	}}
}

type env struct {
	conn   *grpc.ClientConn
	db     *postgres.DB
	intake reportingv1.IntakeServiceClient
	cases  reportingv1.CaseServiceClient
	users  *directory
	logs   *syncBuffer
	now    time.Time
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var (
	pgOnce sync.Once
	pgDSN  string
	pgErr  error
)

// baseDSN starts one Postgres for the package; each test gets its own
// database on it.
func baseDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("DATABASE_TEST_DSN"); dsn != "" {
		return dsn
	}
	pgOnce.Do(func() {
		ctx := context.Background()
		var c *tcpostgres.PostgresContainer
		c, pgErr = tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("reporting"), tcpostgres.WithUsername("reporting"), tcpostgres.WithPassword("reporting"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
		if pgErr != nil {
			return
		}
		pgDSN, pgErr = c.ConnectionString(ctx, "sslmode=disable")
	})
	require.NoError(t, pgErr)
	return pgDSN
}

func newDB(t *testing.T) *postgres.DB {
	t.Helper()
	ctx := context.Background()
	base := baseDSN(t)
	admin, err := pgxpool.New(ctx, base)
	require.NoError(t, err)
	defer admin.Close()
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := "t_" + hex.EncodeToString(b[:])
	_, err = admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, name))
	require.NoError(t, err)
	u, err := url.Parse(base)
	require.NoError(t, err)
	u.Path = "/" + name
	dir, err := filepath.Abs("../../migrations")
	require.NoError(t, err)
	require.NoError(t, postgres.Migrate(u.String(), dir))
	db, err := postgres.New(ctx, u.String())
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db := newDB(t)
	ob, err := outbox.New(outbox.WithTable(auditTable))
	require.NoError(t, err)
	require.NoError(t, ob.Migrate(context.Background(), db))
	e := &env{db: db, users: newDirectory(), logs: &syncBuffer{}, now: time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)}
	lg := log.NewLoggerWithOptions("reporting", log.WithOutput(e.logs), log.WithDefaultLevel(log.LevelTrace))
	deps := grpcsvc.Deps{
		DB: db, Store: store.New(db), Audit: audit.New(store.NewOutboxPublisher(db, ob, audit.ContentType)),
		Officers:   access.New(e.users, []string{officerGroup}),
		NoticeDays: domain.NoticeDays{Affected: 60, Regulator: 60, Media: 60, Other: 30},
		Now:        func() time.Time { return e.now }, Log: lg,
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, lis, lg, server.Options{}, func(s *grpc.Server) {
			reportingv1.RegisterIntakeServiceServer(s, grpcsvc.NewIntake(deps))
			reportingv1.RegisterCaseServiceServer(s, grpcsvc.NewCases(deps))
		})
	}()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(grpcactor.UnaryClientInterceptor()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(server.MaxMessageBytes), grpc.MaxCallSendMsgSize(server.MaxMessageBytes)))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
		cancel()
		<-done
	})
	e.conn = conn
	e.intake = reportingv1.NewIntakeServiceClient(conn)
	e.cases = reportingv1.NewCaseServiceClient(conn)
	return e
}

// as is a call made by the gateway for a signed-in user.
func as(t *testing.T, userID string) context.Context {
	return grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: userID})
}

func details() *reportingv1.ReportDetails {
	return &reportingv1.ReportDetails{
		WhatHappened: "Patient list left on a shared printer\nA printed list sat on the print room printer for about two hours.",
		Occurred:     "About 2 Oct 2026, morning", Location: "Second floor, print room",
		InformationKinds: []reportingv1.InformationKind{reportingv1.InformationKind_INFORMATION_KIND_HEALTH},
		StillHappening:   reportingv1.Answer_ANSWER_NO,
	}
}

// submitAnonymous files an anonymous report and returns its case code and id.
func (e *env) submitAnonymous(t *testing.T) (code, id string) {
	t.Helper()
	r, err := e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: details(), Passphrase: passphrase})
	require.NoError(t, err)
	list, err := e.cases.ListCases(as(t, grace), &reportingv1.ListCasesRequest{})
	require.NoError(t, err)
	for _, c := range list.GetCases() {
		if c.GetCaseCode() == r.GetCaseCode() {
			return r.GetCaseCode(), c.GetId()
		}
	}
	t.Fatalf("case %s is not in the queue", r.GetCaseCode())
	return "", ""
}

func errCode(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	info, ok := apperrgrpc.FromError(err)
	require.True(t, ok, "a coded error: %v", err)
	return info.Code
}

// events decodes every queued audit event, oldest first.
func (e *env) events(t *testing.T) []audit.Event {
	t.Helper()
	rows, err := e.db.Querier().Query(context.Background(), `SELECT payload FROM `+auditTable+` ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []audit.Event
	for rows.Next() {
		var body []byte
		require.NoError(t, rows.Scan(&body))
		ev, err := audit.Decode(body)
		require.NoError(t, err)
		out = append(out, ev)
	}
	require.NoError(t, rows.Err())
	return out
}

func actions(evs []audit.Event) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.Action
	}
	return out
}
