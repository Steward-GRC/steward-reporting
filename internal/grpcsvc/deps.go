// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package grpcsvc serves the reporting API: IntakeService for reporters and
// CaseService for officers. Every view and change is audited in the same
// transaction as the change; reporter-side events name no actor, and no
// event carries report text.
package grpcsvc

import (
	"context"
	"errors"
	"time"

	apperr "github.com/Bugs5382/go-apperr"
	grpcactor "github.com/Bugs5382/go-grpc-actor"
	log "github.com/Bugs5382/go-log"
	postgres "github.com/Bugs5382/go-postgres"

	"github.com/Steward-GRC/steward-reporting/internal/access"
	"github.com/Steward-GRC/steward-reporting/internal/audit"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

// Deps are what the handlers run on.
type Deps struct {
	DB         *postgres.DB
	Store      *store.Store
	Audit      *audit.Emitter
	Officers   *access.Checker
	NoticeDays domain.NoticeDays
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	Log log.Logger
}

func (d Deps) now() time.Time {
	if d.Now == nil {
		return time.Now().UTC()
	}
	return d.Now().UTC()
}

func (d Deps) today() time.Time {
	y, m, day := d.now().Date()
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

func (d Deps) inTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return store.InTx(ctx, d.DB, fn)
}

// emit audits one event; inside inTx it commits with the change.
func (d Deps) emit(ctx context.Context, action, actor, caseID string, attrs map[string]string) error {
	ev := audit.Event{Tier: audit.TierAudit, Action: action, ActorUserID: actor, Attributes: attrs}
	if caseID != "" {
		ev.Subject = "case:" + caseID
	}
	if err := d.Audit.Emit(ctx, ev); err != nil {
		return errcodes.StoreUnavailable("audit "+action, err)
	}
	return nil
}

// signedIn returns the forwarded actor of a named-report or case call. An
// impersonated actor is refused: act-as never reaches reports or cases.
func signedIn(ctx context.Context) (string, error) {
	a, ok := grpcactor.FromContext(ctx)
	if !ok || a.Subject == "" {
		return "", errcodes.SignInRequired()
	}
	if a.Impersonated() {
		return "", errcodes.ActAsNotAllowed()
	}
	return a.Subject, nil
}

// storeErr turns a store error into the coded error a handler returns.
func storeErr(op string, err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return errcodes.CaseNotFound()
	case errors.Is(err, store.ErrClosed):
		return errcodes.CaseClosed()
	}
	if _, ok := apperr.Code(err); ok {
		return err
	}
	return errcodes.StoreUnavailable(op, err)
}

// realUser is the person behind the call: the impersonator during act-as.
func realUser(ctx context.Context) string {
	a, _ := grpcactor.FromContext(ctx)
	return a.RealUser()
}
