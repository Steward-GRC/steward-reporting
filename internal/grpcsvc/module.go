// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc

import (
	"context"
	"errors"
	"strconv"
	"strings"

	log "github.com/Bugs5382/go-log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	identityv1 "github.com/Steward-GRC/steward-reporting/gen/go/thirdparty/identity/v1"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

// HardResetPhrase is the confirmation an admin types, exactly, to run a hard
// reset. The server owns it so no client can weaken the check.
const HardResetPhrase = "RESET COMPLIANCE DATA"

// hardResetModule is the module name identity knows this reset by.
const hardResetModule = "compliance"

// ModuleGate refuses every IntakeService and CaseService call while the
// Compliance module is off, before the handler reads the request, stores
// anything or audits anything. Nothing about the caller is logged: an
// anonymous caller is never identified, and only the method is. Settings,
// legal hold and module calls are not gated, so an admin can switch the
// module back on. Register it in server.Options.Interceptors.
func ModuleGate(d Deps) grpc.UnaryServerInterceptor {
	gated := []string{
		"/" + reportingv1.IntakeService_ServiceDesc.ServiceName + "/",
		"/" + reportingv1.CaseService_ServiceDesc.ServiceName + "/",
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		isGated := false
		for _, p := range gated {
			if strings.HasPrefix(info.FullMethod, p) {
				isGated = true
				break
			}
		}
		if !isGated {
			return next(ctx, req)
		}
		on, err := d.Store.ModuleEnabled(ctx)
		if err != nil {
			d.Log.Ctx(ctx).Error(err, "module switch unreadable, refusing the call", log.F("method", info.FullMethod))
			return nil, errcodes.Error(ctx, errcodes.StoreUnavailable("read module switch", err))
		}
		if !on {
			d.Log.Ctx(ctx).Info("call refused: the compliance module is off", log.F("method", info.FullMethod))
			return nil, errcodes.Error(ctx, errcodes.ModuleOff())
		}
		return next(ctx, req)
	}
}

// Module serves ModuleService.
type Module struct {
	reportingv1.UnimplementedModuleServiceServer
	d Deps
}

// NewModule returns the module switch and hard reset service.
func NewModule(d Deps) *Module { return &Module{d: d} }

// SetComplianceModule turns the module on or off. Off keeps every case.
func (s *Module) SetComplianceModule(ctx context.Context, req *reportingv1.SetComplianceModuleRequest) (*reportingv1.SetComplianceModuleResponse, error) {
	who, err := s.d.settingsAdmin(ctx, "module.access_refused", "")
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	action := "module.disabled"
	if req.GetEnabled() {
		action = "module.enabled"
	}
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		if err := s.d.Store.SetModuleEnabled(ctx, req.GetEnabled(), who); err != nil {
			return errcodes.StoreUnavailable("set module switch", err)
		}
		return s.d.emit(ctx, action, who, "", nil)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	s.d.Log.Ctx(ctx).Info("compliance module switched", log.F("enabled", req.GetEnabled()), log.F("user_id", who))
	return &reportingv1.SetComplianceModuleResponse{ModuleEnabled: req.GetEnabled()}, nil
}

// resetRefused audits a refused hard reset (reason only, no content) and
// returns cause.
func (s *Module) resetRefused(ctx context.Context, who, reason string, cause error) error {
	s.d.Log.Ctx(ctx).Warn("hard reset refused", log.F("user_id", who), log.F("reason", reason))
	if err := s.d.inTx(ctx, func(ctx context.Context) error {
		return s.d.emit(ctx, "module.hard_reset_refused", who, "", map[string]string{"reason": reason})
	}); err != nil {
		return err
	}
	return cause
}

// ExecuteHardReset deletes every case once identity's two-admin approval is
// redeemed. Every check that can fail runs before the approval is spent.
func (s *Module) ExecuteHardReset(ctx context.Context, req *reportingv1.ExecuteHardResetRequest) (*reportingv1.ExecuteHardResetResponse, error) {
	lg := s.d.Log.Ctx(ctx)
	who, err := signedIn(ctx)
	if isCode(err, errcodes.CodeActAsNotAllowed) {
		return nil, errcodes.Error(ctx, s.resetRefused(ctx, realUser(ctx), "act_as", err))
	}
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	lg.Info("hard reset asked for", log.F("user_id", who), log.F("request_id", req.GetRequestId()))

	if err := s.d.Officers.Root(ctx, who); err != nil {
		if isCode(err, errcodes.CodeHardResetRootRequired) {
			return nil, errcodes.Error(ctx, s.resetRefused(ctx, who, "not_root", err))
		}
		return nil, errcodes.Error(ctx, err)
	}
	if req.GetConfirmation() != HardResetPhrase {
		return nil, errcodes.Error(ctx, s.resetRefused(ctx, who, "confirmation", errcodes.HardResetConfirmation()))
	}
	on, err := s.d.Store.ModuleEnabled(ctx)
	if err != nil {
		return nil, errcodes.Error(ctx, errcodes.StoreUnavailable("read module switch", err))
	}
	if on {
		return nil, errcodes.Error(ctx, s.resetRefused(ctx, who, "module_on", errcodes.HardResetModuleOn()))
	}
	held, err := s.d.Store.HoldCount(ctx)
	if err != nil {
		return nil, errcodes.Error(ctx, errcodes.StoreUnavailable("count legal holds", err))
	}
	if held > 0 {
		return nil, errcodes.Error(ctx, s.resetRefused(ctx, who, "legal_hold", errcodes.HardResetLegalHold()))
	}

	// The approval is spent here. A failure after this point burns it: the
	// reset fails closed and a new request needs two admins again.
	resp, err := s.d.HardResets.ConsumeHardReset(ctx, &identityv1.ConsumeHardResetRequest{RequestId: req.GetRequestId(), Module: hardResetModule})
	if err != nil {
		switch status.Code(err) {
		case codes.FailedPrecondition, codes.NotFound, codes.PermissionDenied, codes.InvalidArgument:
			lg.Warn("hard reset approval refused by identity", log.F("request_id", req.GetRequestId()), log.F("code", status.Code(err).String()))
			return nil, errcodes.Error(ctx, s.resetRefused(ctx, who, "not_approved", errcodes.HardResetNotApproved()))
		}
		lg.Error(err, "identity could not redeem the hard reset", log.F("request_id", req.GetRequestId()))
		return nil, errcodes.Error(ctx, errcodes.IdentityUnavailable(err))
	}
	requester, approver := resp.GetRequest().GetRequestedBy(), resp.GetRequest().GetApprovedBy()
	if requester == "" || approver == "" || requester == approver {
		lg.Error(nil, "hard reset approval does not name two different admins, refusing", log.F("request_id", req.GetRequestId()))
		return nil, errcodes.Error(ctx, s.resetRefused(ctx, who, "not_two_admins", errcodes.HardResetNotApproved()))
	}

	var deleted int64
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if deleted, err = s.d.Store.HardResetCases(ctx); err != nil {
			return err
		}
		return s.d.emitSubject(ctx, "module.hard_reset", who, "module:"+hardResetModule, map[string]string{
			"request_id": req.GetRequestId(), "requested_by": requester, "approved_by": approver,
			"cases_deleted": strconv.FormatInt(deleted, 10),
		})
	})
	if err != nil {
		if errors.Is(err, store.ErrHeld) {
			lg.Warn("hard reset stopped by a legal hold placed after the check", log.F("request_id", req.GetRequestId()))
			return nil, errcodes.Error(ctx, s.resetRefused(ctx, who, "legal_hold", errcodes.HardResetLegalHold()))
		}
		lg.Error(err, "hard reset failed after its approval was redeemed", log.F("request_id", req.GetRequestId()))
		return nil, errcodes.Error(ctx, storeErr("hard reset", err))
	}
	lg.Info("compliance module hard reset", log.F("request_id", req.GetRequestId()), log.F("cases_deleted", deleted),
		log.F("requested_by", requester), log.F("approved_by", approver))
	return &reportingv1.ExecuteHardResetResponse{CasesDeleted: deleted, RequestedByUserId: requester, ApprovedByUserId: approver}, nil
}
