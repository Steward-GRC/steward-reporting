// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc_test

import (
	"context"
	"testing"
	"time"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"github.com/stretchr/testify/require"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
	"github.com/Steward-GRC/steward-reporting/internal/retention"
)

// closedCase files a report with an attachment, a thread, a note, an
// assessment, a notice and a corrective action, closes it and backdates the
// close by daysAgo.
func (e *env) closedCase(t *testing.T, daysAgo int) string {
	t.Helper()
	r, err := e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{
		Details: details(), Passphrase: passphrase,
		Attachments: []*reportingv1.AttachmentUpload{{Filename: "note.txt", Data: []byte("About 30 names.")}},
	})
	require.NoError(t, err)
	var id string
	require.NoError(t, e.db.Querier().QueryRow(context.Background(), `SELECT id FROM cases WHERE case_code = $1`,
		r.GetCaseCode()).Scan(&id))
	officer := as(t, grace)
	_, err = e.cases.PostMessage(officer, &reportingv1.PostMessageRequest{CaseId: id, Body: "How many names?"})
	require.NoError(t, err)
	_, err = e.cases.AddNote(officer, &reportingv1.AddNoteRequest{CaseId: id, Body: "Raise with Facilities."})
	require.NoError(t, err)
	_, err = e.cases.RecordRiskAssessment(officer, &reportingv1.RecordRiskAssessmentRequest{CaseId: id, Factors: factors(),
		Decision: reportingv1.BreachDecision_BREACH_DECISION_REPORTABLE, Reason: "Seen by visitors."})
	require.NoError(t, err)
	_, err = e.cases.AddNotice(officer, &reportingv1.AddNoticeRequest{CaseId: id, Recipient: reportingv1.NoticeRecipient_NOTICE_RECIPIENT_AFFECTED_PEOPLE})
	require.NoError(t, err)
	_, err = e.cases.CloseCase(officer, &reportingv1.CloseCaseRequest{CaseId: id, Outcome: reportingv1.Outcome_OUTCOME_SUBSTANTIATED,
		CorrectiveActions: []*reportingv1.CorrectiveAction{{Description: "Badge release on the printers."}}})
	require.NoError(t, err)
	_, err = e.db.Querier().Exec(context.Background(), `UPDATE cases SET closed_at = $2 WHERE id = $1`,
		id, e.now.Add(-time.Duration(daysAgo)*24*time.Hour))
	require.NoError(t, err)
	return id
}

func (e *env) openCase(t *testing.T) string {
	t.Helper()
	r, err := e.intake.SubmitNamedReport(as(t, alice), &reportingv1.SubmitNamedReportRequest{Details: details()})
	require.NoError(t, err)
	_, err = e.db.Querier().Exec(context.Background(), `UPDATE cases SET received_at = $2 WHERE id = $1`,
		r.GetCaseId(), e.now.Add(-20*365*24*time.Hour))
	require.NoError(t, err)
	return r.GetCaseId()
}

// rows counts what is left of a case in every table that holds a part of it.
func (e *env) rows(t *testing.T, id string) int {
	t.Helper()
	var n int
	require.NoError(t, e.db.Querier().QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM cases WHERE id = $1) + (SELECT count(*) FROM case_attachments WHERE case_id = $1) +
		(SELECT count(*) FROM case_messages WHERE case_id = $1) + (SELECT count(*) FROM case_notes WHERE case_id = $1) +
		(SELECT count(*) FROM case_assessments WHERE case_id = $1) + (SELECT count(*) FROM case_notices WHERE case_id = $1) +
		(SELECT count(*) FROM case_corrective_actions WHERE case_id = $1)`, id).Scan(&n))
	return n
}

func (e *env) purger() *retention.Purger {
	return &retention.Purger{DB: e.db, Store: e.store, Audit: e.deps.Audit, Log: e.deps.Log, Now: func() time.Time { return e.now }, Batch: 2}
}

func TestPurgeTakesClosedCasesPastRetentionWithEveryRow(t *testing.T) {
	e := newEnv(t)
	e.save(t, &reportingv1.ComplianceSettings{OfficerGroups: []string{officerGroup}, PublicLinkEnabled: true, RetentionDays: 365})
	old := []string{e.closedCase(t, 400), e.closedCase(t, 500), e.closedCase(t, 366)}
	recent := e.closedCase(t, 364)
	open := e.openCase(t)
	held := e.closedCase(t, 900)
	_, err := e.holds.PlaceLegalHold(as(t, carol), &reportingv1.PlaceLegalHoldRequest{CaseId: held})
	require.NoError(t, err)
	before := len(e.events(t))

	n, err := e.purger().Once(t.Context())
	require.NoError(t, err)
	require.Equal(t, 3, n, "batches of two run until nothing is left")
	for _, id := range old {
		require.Zero(t, e.rows(t, id), "every row of a purged case is gone")
	}
	require.Positive(t, e.rows(t, recent), "a case still inside its retention period is kept")
	require.Positive(t, e.rows(t, open), "an open case is never purged, however old")
	require.Positive(t, e.rows(t, held), "a held case is never purged")

	evs := e.events(t)[before:]
	require.Len(t, evs, 3)
	purged := map[string]bool{}
	for _, ev := range evs {
		require.Equal(t, "case.purged", ev.Action)
		require.Empty(t, ev.ActorUserID)
		require.Equal(t, map[string]string{"retention_days": "365"}, ev.Attributes, "the event carries no content")
		purged[ev.Subject] = true
	}
	for _, id := range old {
		require.True(t, purged["case:"+id])
	}

	n, err = e.purger().Once(t.Context())
	require.NoError(t, err)
	require.Zero(t, n)

	_, err = e.holds.ReleaseLegalHold(as(t, carol), &reportingv1.ReleaseLegalHoldRequest{CaseId: held})
	require.NoError(t, err)
	n, err = e.purger().Once(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, n, "once released, the purge takes it")
	require.Zero(t, e.rows(t, held))
}

func TestPurgeUsesTheDefaultRetention(t *testing.T) {
	e := newEnv(t)
	kept := e.closedCase(t, 7*365-1)
	gone := e.closedCase(t, 7*365+1)
	n, err := e.purger().Once(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Positive(t, e.rows(t, kept))
	require.Zero(t, e.rows(t, gone))
}

// Only one replica purges at a time: a second runner skips while another
// holds the lock.
func TestPurgeRunsOnOneReplicaAtATime(t *testing.T) {
	e := newEnv(t)
	e.closedCase(t, 7*365+1)
	tx, err := e.db.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	var got bool
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, retention.LockName).Scan(&got))
	require.True(t, got)

	n, err := e.purger().Once(t.Context())
	require.NoError(t, err)
	require.Zero(t, n, "the other replica holds the lock")
	require.NoError(t, tx.Rollback(t.Context()))

	n, err = e.purger().Once(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestLegalHolds(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	before := len(e.events(t))

	h, err := e.holds.PlaceLegalHold(as(t, carol), &reportingv1.PlaceLegalHoldRequest{CaseId: id})
	require.NoError(t, err)
	require.Equal(t, id, h.GetHold().GetCaseId())
	require.Equal(t, carol, h.GetHold().GetPlacedByUserId())
	again, err := e.holds.PlaceLegalHold(as(t, dave), &reportingv1.PlaceLegalHoldRequest{CaseId: id})
	require.NoError(t, err)
	require.Equal(t, carol, again.GetHold().GetPlacedByUserId(), "the first hold stands")

	list, err := e.holds.ListLegalHolds(as(t, root), &reportingv1.ListLegalHoldsRequest{})
	require.NoError(t, err)
	require.Len(t, list.GetHolds(), 1)

	c, err := e.cases.GetCase(as(t, grace), &reportingv1.GetCaseRequest{CaseId: id})
	require.NoError(t, err)
	require.True(t, c.GetCase().GetLegalHold(), "officers see the hold")

	_, err = e.holds.ReleaseLegalHold(as(t, carol), &reportingv1.ReleaseLegalHoldRequest{CaseId: id})
	require.NoError(t, err)
	_, err = e.holds.ReleaseLegalHold(as(t, carol), &reportingv1.ReleaseLegalHoldRequest{CaseId: id})
	require.Equal(t, errcodes.CodeCaseNotFound, errCode(t, err), "no hold to release")
	_, err = e.holds.PlaceLegalHold(as(t, carol), &reportingv1.PlaceLegalHoldRequest{CaseId: "0b9f3d0c-1a2b-4c3d-8e4f-0000000000ee"})
	require.Equal(t, errcodes.CodeCaseNotFound, errCode(t, err))
	_, err = e.holds.PlaceLegalHold(as(t, carol), &reportingv1.PlaceLegalHoldRequest{CaseId: "not-a-uuid"})
	require.Equal(t, errcodes.CodeCaseNotFound, errCode(t, err))

	var acts []string
	for _, ev := range e.events(t)[before:] {
		acts = append(acts, ev.Action)
		if ev.Action != "legal_hold.listed" {
			require.Contains(t, []string{"case:" + id, "case:0b9f3d0c-1a2b-4c3d-8e4f-0000000000ee", ""}, ev.Subject)
		}
		require.Empty(t, ev.Attributes, "%s carries no content", ev.Action)
	}
	require.Equal(t, []string{"legal_hold.placed", "legal_hold.listed", "case.viewed", "legal_hold.released"}, acts)
}

func TestOnlySettingsAdminsManageLegalHolds(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	for _, who := range []string{grace, alice} {
		_, err := e.holds.PlaceLegalHold(as(t, who), &reportingv1.PlaceLegalHoldRequest{CaseId: id})
		require.Equal(t, errcodes.CodeSettingsAccessDenied, errCode(t, err), who)
		_, err = e.holds.ListLegalHolds(as(t, who), &reportingv1.ListLegalHoldsRequest{})
		require.Equal(t, errcodes.CodeSettingsAccessDenied, errCode(t, err), who)
	}
	actingAs := grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: carol, Impersonator: dave})
	_, err := e.holds.ReleaseLegalHold(actingAs, &reportingv1.ReleaseLegalHoldRequest{CaseId: id})
	require.Equal(t, errcodes.CodeActAsNotAllowed, errCode(t, err))
}

// The schema refuses to delete a held case, whatever deletes it.
func TestTheSchemaKeepsAHeldCase(t *testing.T) {
	e := newEnv(t)
	_, id := e.submitAnonymous(t)
	_, err := e.holds.PlaceLegalHold(as(t, carol), &reportingv1.PlaceLegalHoldRequest{CaseId: id})
	require.NoError(t, err)
	_, err = e.db.Querier().Exec(context.Background(), `DELETE FROM cases WHERE id = $1`, id)
	require.Error(t, err)
}
