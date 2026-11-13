// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	identityv1 "github.com/Steward-GRC/steward-reporting/gen/go/thirdparty/identity/v1"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
	"github.com/Steward-GRC/steward-reporting/internal/grpcsvc"
)

const resetRequestID = "5a1c1e9e-0000-4000-8000-000000000071"

// fakeHardResets stands in for identity's ConsumeHardReset: identity owns the
// two-person approval, so reporting's tests decide what identity answers.
type fakeHardResets struct {
	mu    sync.Mutex
	calls []*identityv1.ConsumeHardResetRequest
	resp  *identityv1.HardResetRequest
	err   error
}

func (f *fakeHardResets) approved(requestedBy, approvedBy string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = nil
	f.resp = &identityv1.HardResetRequest{Id: resetRequestID, Module: "compliance",
		State: identityv1.HardResetState_HARD_RESET_STATE_CONSUMED, RequestedBy: requestedBy, ApprovedBy: approvedBy}
}

func (f *fakeHardResets) refuse(c codes.Code) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = status.Error(c, "identity refused")
}

func (f *fakeHardResets) consumed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeHardResets) ConsumeHardReset(_ context.Context, in *identityv1.ConsumeHardResetRequest, _ ...grpc.CallOption) (*identityv1.ConsumeHardResetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, in)
	if f.err != nil {
		return nil, f.err
	}
	return &identityv1.ConsumeHardResetResponse{Request: f.resp}, nil
}

func (e *env) setModule(t *testing.T, who string, on bool) error {
	t.Helper()
	_, err := e.module.SetComplianceModule(as(t, who), &reportingv1.SetComplianceModuleRequest{Enabled: on})
	return err
}

func (e *env) caseCount(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, e.db.Querier().QueryRow(context.Background(), `SELECT count(*) FROM cases`).Scan(&n))
	return n
}

func (e *env) reset(t *testing.T, who, phrase string) (*reportingv1.ExecuteHardResetResponse, error) {
	t.Helper()
	return e.module.ExecuteHardReset(as(t, who), &reportingv1.ExecuteHardResetRequest{RequestId: resetRequestID, Confirmation: phrase})
}

const phrase = "RESET COMPLIANCE DATA"

func TestModuleStartsOn(t *testing.T) {
	e := newEnv(t)
	r, err := e.sets.GetSettings(as(t, carol), &reportingv1.GetSettingsRequest{})
	require.NoError(t, err)
	require.True(t, r.GetSettings().GetModuleEnabled())
}

func TestOffSwitchRefusesEveryIntakeAndCaseRPCAndKeepsTheData(t *testing.T) {
	e := newEnv(t)
	code, _ := e.submitAnonymous(t)
	require.NoError(t, e.setModule(t, carol, false))
	before := len(e.events(t))

	var methods []string
	for _, d := range []grpc.ServiceDesc{reportingv1.IntakeService_ServiceDesc, reportingv1.CaseService_ServiceDesc} {
		for _, m := range d.Methods {
			methods = append(methods, "/"+d.ServiceName+"/"+m.MethodName)
		}
	}
	require.GreaterOrEqual(t, len(methods), 20)
	for _, m := range methods {
		err := e.conn.Invoke(as(t, grace), m, &emptypb.Empty{}, &emptypb.Empty{})
		require.Equal(t, errcodes.CodeModuleOff, errCode(t, err), m)
	}

	_, err := e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: details(), Passphrase: passphrase})
	require.Equal(t, errcodes.CodeModuleOff, errCode(t, err))
	_, err = e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.Equal(t, errcodes.CodeModuleOff, errCode(t, err))

	require.Equal(t, 1, e.caseCount(t), "nothing was stored or deleted")
	require.Len(t, e.events(t), before, "a refused call audits nothing")

	require.NoError(t, e.setModule(t, carol, true))
	got, err := e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestOffSwitchLeavesNothingOfTheCallerInLogsOrAudit(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.setModule(t, carol, false))
	before := len(e.events(t))

	ctx := grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: alice})
	_, err := e.intake.SubmitAnonymousReport(ctx, &reportingv1.SubmitAnonymousReportRequest{Details: details(), Passphrase: passphrase})
	require.Equal(t, errcodes.CodeModuleOff, errCode(t, err))

	require.Len(t, e.events(t), before)
	require.NotContains(t, e.logs.String(), alice)
	require.NotContains(t, e.logs.String(), passphrase)
}

func TestSettingsLegalHoldsAndTheSwitchStayAvailableWhileOff(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	require.NoError(t, e.setModule(t, carol, false))

	r, err := e.sets.GetSettings(as(t, carol), &reportingv1.GetSettingsRequest{})
	require.NoError(t, err)
	require.False(t, r.GetSettings().GetModuleEnabled())

	_, err = e.holds.PlaceLegalHold(as(t, carol), &reportingv1.PlaceLegalHoldRequest{CaseId: id})
	require.NoError(t, err)
	_, err = e.holds.ListLegalHolds(as(t, carol), &reportingv1.ListLegalHoldsRequest{})
	require.NoError(t, err)

	saved := e.save(t, &reportingv1.ComplianceSettings{PublicLinkEnabled: true, RetentionDays: 400})
	require.False(t, saved.GetModuleEnabled(), "UpdateSettings never turns the module back on")
	require.NoError(t, e.setModule(t, carol, true))
	r, err = e.sets.GetSettings(as(t, carol), &reportingv1.GetSettingsRequest{})
	require.NoError(t, err)
	require.True(t, r.GetSettings().GetModuleEnabled())
}

func TestOnlySettingsAdminsSwitchTheModule(t *testing.T) {
	e := newEnv(t)
	require.Equal(t, errcodes.CodeSettingsAccessDenied, errCode(t, e.setModule(t, bob, false)))
	require.Equal(t, errcodes.CodeSettingsAccessDenied, errCode(t, e.setModule(t, grace, false)))

	_, err := e.module.SetComplianceModule(t.Context(), &reportingv1.SetComplianceModuleRequest{})
	require.Equal(t, errcodes.CodeSignInRequired, errCode(t, err))
	ctx := grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: bob, Impersonator: carol})
	_, err = e.module.SetComplianceModule(ctx, &reportingv1.SetComplianceModuleRequest{})
	require.Equal(t, errcodes.CodeActAsNotAllowed, errCode(t, err))

	on, err := e.store.ModuleEnabled(t.Context())
	require.NoError(t, err)
	require.True(t, on)

	require.NoError(t, e.setModule(t, dave, false))
	last := e.events(t)[len(e.events(t))-1]
	require.Equal(t, "module.disabled", last.Action)
	require.Equal(t, dave, last.ActorUserID)
	require.NoError(t, e.setModule(t, carol, true))
	last = e.events(t)[len(e.events(t))-1]
	require.Equal(t, "module.enabled", last.Action)
}

func TestHardResetIsRefusedBeforeAnythingIsRedeemed(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	e.resets.approved(root, dave)

	cases := []struct {
		name  string
		setup func()
		who   string
		text  string
		code  int
	}{
		{"module still on", func() {}, root, phrase, errcodes.CodeHardResetModuleOn},
		{"wrong phrase", func() { require.NoError(t, e.setModule(t, carol, false)) }, root, "reset compliance data", errcodes.CodeHardResetConfirmation},
		{"empty phrase", func() {}, root, "", errcodes.CodeHardResetConfirmation},
		{"site admin is not root", func() {}, dave, phrase, errcodes.CodeHardResetRootRequired},
		{"compliance admin is not root", func() {}, carol, phrase, errcodes.CodeHardResetRootRequired},
		{"a legal hold blocks it", func() {
			_, err := e.holds.PlaceLegalHold(as(t, carol), &reportingv1.PlaceLegalHoldRequest{CaseId: id})
			require.NoError(t, err)
		}, root, phrase, errcodes.CodeHardResetLegalHold},
	}
	for _, c := range cases {
		c.setup()
		_, err := e.reset(t, c.who, c.text)
		require.Equal(t, c.code, errCode(t, err), c.name)
		require.Zero(t, e.resets.consumed(), c.name+": the approval is not spent")
		require.Equal(t, 1, e.caseCount(t), c.name)
	}

	_, err := e.module.ExecuteHardReset(t.Context(), &reportingv1.ExecuteHardResetRequest{RequestId: resetRequestID, Confirmation: phrase})
	require.Equal(t, errcodes.CodeSignInRequired, errCode(t, err))
	ctx := grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: root, Impersonator: dave})
	_, err = e.module.ExecuteHardReset(ctx, &reportingv1.ExecuteHardResetRequest{RequestId: resetRequestID, Confirmation: phrase})
	require.Equal(t, errcodes.CodeActAsNotAllowed, errCode(t, err))
	require.Zero(t, e.resets.consumed())
}

func TestHardResetNeedsIdentitysApprovalFromADifferentAdmin(t *testing.T) {
	e := newEnv(t)
	e.submitAnonymous(t)
	require.NoError(t, e.setModule(t, carol, false))

	for _, c := range []codes.Code{codes.FailedPrecondition, codes.NotFound, codes.PermissionDenied} {
		e.resets.refuse(c)
		_, err := e.reset(t, root, phrase)
		require.Equal(t, errcodes.CodeHardResetNotApproved, errCode(t, err), c.String())
	}
	e.resets.refuse(codes.Unavailable)
	_, err := e.reset(t, root, phrase)
	require.Equal(t, errcodes.CodeIdentityUnavailable, errCode(t, err))

	e.resets.approved(root, root)
	_, err = e.reset(t, root, phrase)
	require.Equal(t, errcodes.CodeHardResetNotApproved, errCode(t, err), "the same admin on both sides is never four-eyes")

	e.resets.approved(root, "")
	_, err = e.reset(t, root, phrase)
	require.Equal(t, errcodes.CodeHardResetNotApproved, errCode(t, err), "an approval that names no second admin is refused")

	require.Equal(t, 1, e.caseCount(t), "every refusal kept the data")
	for _, c := range e.resets.calls {
		require.Equal(t, "compliance", c.GetModule())
		require.Equal(t, resetRequestID, c.GetRequestId())
	}
}

func TestHardResetDeletesEverythingAndLeavesAContentFreeTombstone(t *testing.T) {
	e := newEnv(t)
	code, id := e.submitAnonymous(t)
	e.submitAnonymous(t)
	_, err := e.intake.SubmitNamedReport(as(t, alice), &reportingv1.SubmitNamedReportRequest{Details: details()})
	require.NoError(t, err)
	_, err = e.cases.AddNote(as(t, grace), &reportingv1.AddNoteRequest{CaseId: id, Body: "an internal note"})
	require.NoError(t, err)
	require.Equal(t, 3, e.caseCount(t))
	require.NoError(t, e.setModule(t, carol, false))
	e.resets.approved(heidi, dave)
	before := len(e.events(t))
	logged := len(e.logs.String())

	r, err := e.reset(t, root, phrase)
	require.NoError(t, err)
	require.EqualValues(t, 3, r.GetCasesDeleted())
	require.Equal(t, heidi, r.GetRequestedByUserId())
	require.Equal(t, dave, r.GetApprovedByUserId())
	require.Equal(t, 1, e.resets.consumed())
	require.Zero(t, e.caseCount(t))

	for _, table := range []string{"case_notes", "case_messages", "case_attachments", "case_assessments", "case_notices", "case_corrective_actions", "case_legal_holds"} {
		var n int
		require.NoError(t, e.db.Querier().QueryRow(t.Context(), `SELECT count(*) FROM `+table).Scan(&n))
		require.Zero(t, n, table)
	}
	on, err := e.store.ModuleEnabled(t.Context())
	require.NoError(t, err)
	require.False(t, on, "the module stays off after a reset")
	_, err = e.store.Settings(t.Context())
	require.NoError(t, err, "the settings survive")

	evs := e.events(t)
	require.Len(t, evs, before+1, "one tombstone, nothing per case")
	tomb := evs[len(evs)-1]
	require.Equal(t, "module.hard_reset", tomb.Action)
	require.Equal(t, root, tomb.ActorUserID)
	require.Equal(t, "module:compliance", tomb.Subject)
	require.Equal(t, map[string]string{
		"request_id": resetRequestID, "requested_by": heidi, "approved_by": dave, "cases_deleted": "3",
	}, tomb.Attributes)
	raw, err := json.Marshal(tomb)
	require.NoError(t, err)
	for _, secret := range []string{id, code, alice, "Patient list", passphrase, "an internal note"} {
		require.NotContains(t, string(raw), secret)
		require.NotContains(t, e.logs.String()[logged:], secret, "the reset logs nothing a case held")
	}

	e.resets.refuse(codes.FailedPrecondition)
	_, err = e.reset(t, root, phrase)
	require.Equal(t, errcodes.CodeHardResetNotApproved, errCode(t, err), "an approval is used once")
}

func TestHardResetWorksAfterTheHoldIsReleased(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	require.NoError(t, e.setModule(t, carol, false))
	e.resets.approved(heidi, dave)
	_, err := e.holds.PlaceLegalHold(as(t, carol), &reportingv1.PlaceLegalHoldRequest{CaseId: id})
	require.NoError(t, err)
	_, err = e.reset(t, root, phrase)
	require.Equal(t, errcodes.CodeHardResetLegalHold, errCode(t, err))
	_, err = e.holds.ReleaseLegalHold(as(t, carol), &reportingv1.ReleaseLegalHoldRequest{CaseId: id})
	require.NoError(t, err)
	r, err := e.reset(t, root, phrase)
	require.NoError(t, err)
	require.EqualValues(t, 1, r.GetCasesDeleted())
}

func TestModuleServicesAreOnTheCallerPolicy(t *testing.T) {
	for _, d := range []grpc.ServiceDesc{reportingv1.ModuleService_ServiceDesc} {
		for _, m := range d.Methods {
			_, ok := grpcsvc.CallerPolicy["/"+d.ServiceName+"/"+m.MethodName]
			require.True(t, ok, m.MethodName)
		}
	}
}
