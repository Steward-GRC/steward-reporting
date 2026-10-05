// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc_test

import (
	"testing"
	"time"

	"github.com/Bugs5382/go-apperr/apperrgrpc"
	"github.com/stretchr/testify/require"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

func factors() *reportingv1.RiskFactors {
	return &reportingv1.RiskFactors{
		Information: []reportingv1.InformationKind{reportingv1.InformationKind_INFORMATION_KIND_HEALTH},
		Recipient:   reportingv1.Recipient_RECIPIENT_UNKNOWN_PEOPLE,
		Viewed:      reportingv1.Viewed_VIEWED_PROBABLY,
		Mitigation:  reportingv1.Mitigation_MITIGATION_PARTLY,
	}
}

func TestQueueFiltersAndCounts(t *testing.T) {
	e := newEnv(t)
	officer := as(t, grace)
	_, a := e.submitAnonymous(t)
	_, b := e.submitAnonymous(t)
	_, err := e.cases.AssignCase(officer, &reportingv1.AssignCaseRequest{CaseId: b, AssigneeUserId: heidi})
	require.NoError(t, err)
	_, err = e.cases.SetCaseStatus(officer, &reportingv1.SetCaseStatusRequest{CaseId: b, Status: reportingv1.CaseStatus_CASE_STATUS_IN_REVIEW})
	require.NoError(t, err)

	all, err := e.cases.ListCases(officer, &reportingv1.ListCasesRequest{})
	require.NoError(t, err)
	require.Len(t, all.GetCases(), 2)
	counts := map[reportingv1.CaseStatus]int32{}
	for _, c := range all.GetCounts() {
		counts[c.GetStatus()] = c.GetCount()
	}
	require.Equal(t, int32(1), counts[reportingv1.CaseStatus_CASE_STATUS_NEW])
	require.Equal(t, int32(1), counts[reportingv1.CaseStatus_CASE_STATUS_IN_REVIEW])
	require.Equal(t, int32(0), counts[reportingv1.CaseStatus_CASE_STATUS_CLOSED], "every status is counted, empty ones too")
	require.Len(t, all.GetCounts(), 6)

	news, err := e.cases.ListCases(officer, &reportingv1.ListCasesRequest{Statuses: []reportingv1.CaseStatus{reportingv1.CaseStatus_CASE_STATUS_NEW}})
	require.NoError(t, err)
	require.Len(t, news.GetCases(), 1)
	require.Equal(t, a, news.GetCases()[0].GetId())
	require.Equal(t, "Patient list left on a shared printer", news.GetCases()[0].GetSummary())
	heidis, err := e.cases.ListCases(officer, &reportingv1.ListCasesRequest{AssigneeUserId: heidi})
	require.NoError(t, err)
	require.Len(t, heidis.GetCases(), 1)
	require.Equal(t, b, heidis.GetCases()[0].GetId())

	_, err = e.cases.SetCaseStatus(officer, &reportingv1.SetCaseStatusRequest{CaseId: a, Status: reportingv1.CaseStatus_CASE_STATUS_CLOSED})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err), "closing goes through close-out")
	_, err = e.cases.GetCase(officer, &reportingv1.GetCaseRequest{CaseId: "0b9f3d0c-1a2b-4c3d-8e4f-00000000dead"})
	require.Equal(t, errcodes.CodeCaseNotFound, errCode(t, err))
}

func TestCasesAreAssignedToOfficersOnly(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	_, err := e.cases.AssignCase(as(t, grace), &reportingv1.AssignCaseRequest{CaseId: id, AssigneeUserId: dave})
	info, ok := apperrgrpc.FromError(err)
	require.True(t, ok)
	require.Equal(t, errcodes.CodeInvalid, info.Code)
	require.Equal(t, "assignee_user_id", info.Metadata["field"])
	got, err := e.cases.AssignCase(as(t, grace), &reportingv1.AssignCaseRequest{CaseId: id, AssigneeUserId: heidi})
	require.NoError(t, err)
	require.Equal(t, heidi, got.GetCase().GetAssigneeUserId())
	got, err = e.cases.AssignCase(as(t, grace), &reportingv1.AssignCaseRequest{CaseId: id})
	require.NoError(t, err)
	require.Empty(t, got.GetCase().GetAssigneeUserId(), "empty clears the assignee")
}

func TestNotesAndTheThreadStaySeparate(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	n, err := e.cases.AddNote(as(t, grace), &reportingv1.AddNoteRequest{CaseId: id, Body: "Raise with Facilities."})
	require.NoError(t, err)
	require.Equal(t, grace, n.GetNote().GetAuthorUserId())
	_, err = e.cases.AddNote(as(t, grace), &reportingv1.AddNoteRequest{CaseId: id, Body: "  "})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err))
	c, err := e.cases.GetCase(as(t, grace), &reportingv1.GetCaseRequest{CaseId: id})
	require.NoError(t, err)
	require.Len(t, c.GetCase().GetNotes(), 1)
	require.Empty(t, c.GetCase().GetThread())
}

func TestRiskAssessmentEndsInARecordedDecision(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	officer := as(t, grace)
	_, err := e.cases.RecordRiskAssessment(officer, &reportingv1.RecordRiskAssessmentRequest{CaseId: id, Factors: factors(), Decision: reportingv1.BreachDecision_BREACH_DECISION_REPORTABLE})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err), "the reason is required")

	low := factors()
	low.Mitigation = reportingv1.Mitigation_MITIGATION_FULLY
	got, err := e.cases.RecordRiskAssessment(officer, &reportingv1.RecordRiskAssessmentRequest{CaseId: id, Factors: low, Decision: reportingv1.BreachDecision_BREACH_DECISION_NOT_REPORTABLE, Reason: "Shredded at once."})
	require.NoError(t, err)
	require.Equal(t, reportingv1.Suggestion_SUGGESTION_LOW_PROBABILITY_OF_COMPROMISE, got.GetAssessment().GetSuggestion())
	c, err := e.cases.GetCase(officer, &reportingv1.GetCaseRequest{CaseId: id})
	require.NoError(t, err)
	require.Equal(t, reportingv1.CaseStatus_CASE_STATUS_IN_REVIEW, c.GetCase().GetStatus())

	got, err = e.cases.RecordRiskAssessment(officer, &reportingv1.RecordRiskAssessmentRequest{CaseId: id, Factors: factors(), Decision: reportingv1.BreachDecision_BREACH_DECISION_REPORTABLE, Reason: "Unattended for about 2 hours."})
	require.NoError(t, err)
	require.Equal(t, reportingv1.Suggestion_SUGGESTION_NOTIFICATION_LIKELY_REQUIRED, got.GetAssessment().GetSuggestion())
	require.Equal(t, grace, got.GetAssessment().GetDecidedByUserId())
	c, err = e.cases.GetCase(officer, &reportingv1.GetCaseRequest{CaseId: id})
	require.NoError(t, err)
	require.Equal(t, reportingv1.CaseStatus_CASE_STATUS_NOTIFICATION_DUE, c.GetCase().GetStatus())
	require.Equal(t, "Unattended for about 2 hours.", c.GetCase().GetAssessment().GetReason(), "the latest decision is the case's")
}

func TestNoticeDeadlinesCountFromDiscovery(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	officer := as(t, grace)
	_, err := e.cases.SetDiscoveryDate(officer, &reportingv1.SetDiscoveryDateRequest{CaseId: id, DiscoveredOn: "2026-10-06"})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err), "discovery can't be in the future")
	c, err := e.cases.SetDiscoveryDate(officer, &reportingv1.SetDiscoveryDateRequest{CaseId: id, DiscoveredOn: "2026-10-02"})
	require.NoError(t, err)
	require.Equal(t, "2026-10-02", c.GetCase().GetDiscoveredOn())

	reg, err := e.cases.AddNotice(officer, &reportingv1.AddNoticeRequest{CaseId: id, Recipient: reportingv1.NoticeRecipient_NOTICE_RECIPIENT_REGULATOR, Method: "Online form"})
	require.NoError(t, err)
	require.Equal(t, int32(60), reg.GetNotice().GetDaysAllowed())
	require.Equal(t, "2026-12-01", reg.GetNotice().GetDueOn())
	require.Equal(t, reportingv1.NoticeStatus_NOTICE_STATUS_NOT_SENT, reg.GetNotice().GetStatus())
	other, err := e.cases.AddNotice(officer, &reportingv1.AddNoticeRequest{CaseId: id, Recipient: reportingv1.NoticeRecipient_NOTICE_RECIPIENT_OTHER, Label: "Business associate"})
	require.NoError(t, err)
	require.Equal(t, "2026-11-01", other.GetNotice().GetDueOn(), "days come from the recipient kind's setting")

	list, err := e.cases.ListCases(officer, &reportingv1.ListCasesRequest{})
	require.NoError(t, err)
	require.Equal(t, "2026-11-01", list.GetCases()[0].GetNextDeadline())

	c, err = e.cases.SetDiscoveryDate(officer, &reportingv1.SetDiscoveryDateRequest{CaseId: id, DiscoveredOn: "2026-09-30"})
	require.NoError(t, err)
	require.Equal(t, "2026-11-29", c.GetCase().GetNotices()[0].GetDueOn(), "a new discovery date moves every deadline")

	_, err = e.cases.UpdateNotice(officer, &reportingv1.UpdateNoticeRequest{CaseId: id, NoticeId: reg.GetNotice().GetId(), Status: reportingv1.NoticeStatus_NOTICE_STATUS_SENT})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err), "a sent notice needs its date")
	sent, err := e.cases.UpdateNotice(officer, &reportingv1.UpdateNoticeRequest{CaseId: id, NoticeId: reg.GetNotice().GetId(), Status: reportingv1.NoticeStatus_NOTICE_STATUS_SENT, SentOn: "2026-10-05"})
	require.NoError(t, err)
	require.Equal(t, "2026-10-05", sent.GetNotice().GetSentOn())
	_, other2 := e.submitAnonymous(t)
	_, err = e.cases.UpdateNotice(officer, &reportingv1.UpdateNoticeRequest{CaseId: other2, NoticeId: reg.GetNotice().GetId(), Status: reportingv1.NoticeStatus_NOTICE_STATUS_DRAFT})
	require.Equal(t, errcodes.CodeCaseNotFound, errCode(t, err), "a notice is reached only through its case")
}

func TestCloseOutRecordsActionsAndTellsTheReporter(t *testing.T) {
	e := newEnv(t)
	code, id := e.submitAnonymous(t)
	officer := as(t, grace)
	const policy = "5b0b7f0e-7a51-4d38-9a0c-2f4b1d7f6a10"
	_, err := e.cases.CloseCase(officer, &reportingv1.CloseCaseRequest{CaseId: id, Outcome: reportingv1.Outcome_OUTCOME_SUBSTANTIATED,
		CorrectiveActions: []*reportingv1.CorrectiveAction{{Description: "Badge release", PolicyId: "VSP-001"}}})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err), "a policy link is core's policy id")

	c, err := e.cases.CloseCase(officer, &reportingv1.CloseCaseRequest{
		CaseId: id, Outcome: reportingv1.Outcome_OUTCOME_SUBSTANTIATED,
		CorrectiveActions: []*reportingv1.CorrectiveAction{
			{Description: "Badge release on print room printers", PolicyId: policy},
			{Description: "Remind staff about printouts"},
		},
		ClosingMessage: "Thank you for speaking up. We've fixed how the printer releases documents.",
	})
	require.NoError(t, err)
	require.Equal(t, reportingv1.CaseStatus_CASE_STATUS_CLOSED, c.GetCase().GetStatus())
	require.Equal(t, reportingv1.Outcome_OUTCOME_SUBSTANTIATED, c.GetCase().GetOutcome())
	require.Len(t, c.GetCase().GetCorrectiveActions(), 2)
	require.Equal(t, policy, c.GetCase().GetCorrectiveActions()[0].GetPolicyId())
	require.WithinDuration(t, time.Now(), c.GetCase().GetClosedAt().AsTime(), time.Minute)

	view, err := e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.NoError(t, err)
	require.Equal(t, "Thank you for speaking up. We've fixed how the printer releases documents.", view.GetReport().GetThread()[0].GetBody())

	for name, call := range map[string]func() error{
		"note": func() error {
			_, err := e.cases.AddNote(officer, &reportingv1.AddNoteRequest{CaseId: id, Body: "late"})
			return err
		},
		"message": func() error {
			_, err := e.cases.PostMessage(officer, &reportingv1.PostMessageRequest{CaseId: id, Body: "late"})
			return err
		},
		"close": func() error {
			_, err := e.cases.CloseCase(officer, &reportingv1.CloseCaseRequest{CaseId: id, Outcome: reportingv1.Outcome_OUTCOME_INCONCLUSIVE})
			return err
		},
		"status": func() error {
			_, err := e.cases.SetCaseStatus(officer, &reportingv1.SetCaseStatusRequest{CaseId: id, Status: reportingv1.CaseStatus_CASE_STATUS_IN_REVIEW})
			return err
		},
	} {
		require.Equal(t, errcodes.CodeCaseClosed, errCode(t, call()), name)
	}
	_, err = e.cases.GetCase(officer, &reportingv1.GetCaseRequest{CaseId: id})
	require.NoError(t, err, "a closed case can still be read")
}

// Every view and change is audited with the officer as the actor; the
// reporter's side names no actor.
func TestEveryViewAndChangeIsAudited(t *testing.T) {
	e := newEnv(t)
	officer := as(t, grace)
	code, id := e.submitAnonymous(t)
	_, err := e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.NoError(t, err)
	_, err = e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: "wrong passphrase"})
	require.Error(t, err)
	_, err = e.cases.GetCase(officer, &reportingv1.GetCaseRequest{CaseId: id})
	require.NoError(t, err)
	_, err = e.cases.PostMessage(officer, &reportingv1.PostMessageRequest{CaseId: id, Body: "Thank you."})
	require.NoError(t, err)
	_, err = e.intake.ReplyToReport(t.Context(), &reportingv1.ReplyToReportRequest{CaseCode: code, Passphrase: passphrase, Body: "Sure."})
	require.NoError(t, err)
	_, err = e.cases.AddNote(officer, &reportingv1.AddNoteRequest{CaseId: id, Body: "note"})
	require.NoError(t, err)
	_, err = e.cases.AssignCase(officer, &reportingv1.AssignCaseRequest{CaseId: id, AssigneeUserId: grace})
	require.NoError(t, err)
	_, err = e.cases.SetCaseStatus(officer, &reportingv1.SetCaseStatusRequest{CaseId: id, Status: reportingv1.CaseStatus_CASE_STATUS_RISK_ASSESSMENT})
	require.NoError(t, err)
	_, err = e.cases.SetDiscoveryDate(officer, &reportingv1.SetDiscoveryDateRequest{CaseId: id, DiscoveredOn: "2026-10-02"})
	require.NoError(t, err)
	_, err = e.cases.RecordRiskAssessment(officer, &reportingv1.RecordRiskAssessmentRequest{CaseId: id, Factors: factors(), Decision: reportingv1.BreachDecision_BREACH_DECISION_REPORTABLE, Reason: "why"})
	require.NoError(t, err)
	n, err := e.cases.AddNotice(officer, &reportingv1.AddNoticeRequest{CaseId: id, Recipient: reportingv1.NoticeRecipient_NOTICE_RECIPIENT_REGULATOR})
	require.NoError(t, err)
	_, err = e.cases.UpdateNotice(officer, &reportingv1.UpdateNoticeRequest{CaseId: id, NoticeId: n.GetNotice().GetId(), Status: reportingv1.NoticeStatus_NOTICE_STATUS_DRAFT})
	require.NoError(t, err)
	_, err = e.cases.CloseCase(officer, &reportingv1.CloseCaseRequest{CaseId: id, Outcome: reportingv1.Outcome_OUTCOME_SUBSTANTIATED})
	require.NoError(t, err)

	evs := e.events(t)
	require.Equal(t, []string{
		"report.submitted", "case.listed", "report.checked", "report.check_refused", "case.viewed", "case.message_posted",
		"report.replied", "case.note_added", "case.assigned", "case.status_changed", "case.discovery_date_set",
		"case.assessment_recorded", "case.notice_added", "case.notice_updated", "case.closed",
	}, actions(evs))
	for _, ev := range evs {
		switch ev.Action[:5] {
		case "case.":
			require.Equal(t, grace, ev.ActorUserID, ev.Action)
			if ev.Action != "case.listed" {
				require.Equal(t, "case:"+id, ev.Subject, ev.Action)
			}
		default:
			require.Empty(t, ev.ActorUserID, ev.Action)
		}
	}
}
