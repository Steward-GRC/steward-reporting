// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc

import (
	"context"

	log "github.com/Bugs5382/go-log"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

// LegalHolds serves LegalHoldService, for the same admins as the settings.
type LegalHolds struct {
	reportingv1.UnimplementedLegalHoldServiceServer
	d Deps
}

// NewLegalHolds returns the legal hold service.
func NewLegalHolds(d Deps) *LegalHolds { return &LegalHolds{d: d} }

func holdToProto(h store.Hold) *reportingv1.LegalHold {
	return &reportingv1.LegalHold{CaseId: h.CaseID, PlacedByUserId: h.PlacedBy, PlacedAt: ts(h.PlacedAt)}
}

// PlaceLegalHold puts a case under a legal hold.
func (s *LegalHolds) PlaceLegalHold(ctx context.Context, req *reportingv1.PlaceLegalHoldRequest) (*reportingv1.PlaceLegalHoldResponse, error) {
	who, err := s.d.settingsAdmin(ctx, "legal_hold.access_refused", req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var h store.Hold
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var placed bool
		var err error
		if h, placed, err = s.d.Store.PlaceHold(ctx, req.GetCaseId(), who); err != nil || !placed {
			return err
		}
		return s.d.emit(ctx, "legal_hold.placed", who, req.GetCaseId(), nil)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("place legal hold", err))
	}
	s.d.Log.Ctx(ctx).Info("legal hold placed", log.F("case_id", h.CaseID), log.F("user_id", who))
	return &reportingv1.PlaceLegalHoldResponse{Hold: holdToProto(h)}, nil
}

// ReleaseLegalHold lifts a case's legal hold.
func (s *LegalHolds) ReleaseLegalHold(ctx context.Context, req *reportingv1.ReleaseLegalHoldRequest) (*reportingv1.ReleaseLegalHoldResponse, error) {
	who, err := s.d.settingsAdmin(ctx, "legal_hold.access_refused", req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		if err := s.d.Store.ReleaseHold(ctx, req.GetCaseId()); err != nil {
			return err
		}
		return s.d.emit(ctx, "legal_hold.released", who, req.GetCaseId(), nil)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("release legal hold", err))
	}
	s.d.Log.Ctx(ctx).Info("legal hold released", log.F("case_id", req.GetCaseId()), log.F("user_id", who))
	return &reportingv1.ReleaseLegalHoldResponse{}, nil
}

// ListLegalHolds lists every held case.
func (s *LegalHolds) ListLegalHolds(ctx context.Context, _ *reportingv1.ListLegalHoldsRequest) (*reportingv1.ListLegalHoldsResponse, error) {
	who, err := s.d.settingsAdmin(ctx, "legal_hold.access_refused", "")
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var holds []store.Hold
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if holds, err = s.d.Store.Holds(ctx); err != nil {
			return err
		}
		return s.d.emit(ctx, "legal_hold.listed", who, "", nil)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("list legal holds", err))
	}
	out := &reportingv1.ListLegalHoldsResponse{}
	for _, h := range holds {
		out.Holds = append(out.Holds, holdToProto(h))
	}
	return out, nil
}
