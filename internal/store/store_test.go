// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	outbox "github.com/Bugs5382/go-outbox"
	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

const (
	alice = "0b9f3d0c-1a2b-4c3d-8e4f-000000000001"
	bob   = "0b9f3d0c-1a2b-4c3d-8e4f-000000000002"
	grace = "0b9f3d0c-1a2b-4c3d-8e4f-000000000007"
)

func day(s string) time.Time {
	d, err := time.Parse(domain.DateLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

func details() domain.Details {
	return domain.Details{
		WhatHappened: "Patient list left on a shared printer\nA printed list sat unattended for two hours.",
		Occurred:     "About 2 Oct 2026, morning", Location: "Second floor, print room",
		InformationKinds: []domain.InformationKind{domain.InfoHealth}, StillHappening: domain.AnswerNo,
	}
}

func anonymous(code string) store.NewCase {
	return store.NewCase{Kind: domain.KindAnonymous, CaseCode: code, PassphraseHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		Details: details(), DiscoveredOn: day("2026-10-02")}
}

func named(code, reporter string) store.NewCase {
	return store.NewCase{Kind: domain.KindNamed, CaseCode: code, ReporterUserID: reporter,
		Details: details(), DiscoveredOn: day("2026-10-02")}
}

func TestAnonymousCaseIsFoundOnlyByItsCode(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	id, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)

	got, hash, err := s.FindAnonymous(ctx, "7KQ-42M-RX")
	require.NoError(t, err)
	require.Equal(t, id, got)
	require.Equal(t, "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA", hash)

	_, _, err = s.FindAnonymous(ctx, "7KQ-42M-RY")
	require.ErrorIs(t, err, store.ErrNotFound)

	_, err = s.Create(ctx, named("N22-222-22", alice))
	require.NoError(t, err)
	_, _, err = s.FindAnonymous(ctx, "N22-222-22")
	require.ErrorIs(t, err, store.ErrNotFound, "a named case is never opened by code")
}

func TestTheSchemaRefusesAReporterOnAnAnonymousCase(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	_, err := db.Querier().Exec(ctx, `INSERT INTO cases (case_code, kind, reporter_user_id, passphrase_hash, what_happened, discovered_on)
		VALUES ('AAA-AAA-AA', 'anonymous', $1, 'h', 'x', '2026-10-02')`, alice)
	require.ErrorContains(t, err, "cases_reporter_matches_kind")
	_, err = db.Querier().Exec(ctx, `INSERT INTO cases (case_code, kind, what_happened, discovered_on)
		VALUES ('AAA-AAA-AB', 'anonymous', 'x', '2026-10-02')`)
	require.ErrorContains(t, err, "cases_reporter_matches_kind", "an anonymous case needs its passphrase hash")
}

func TestCaseCodesAreUnique(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	_, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)
	_, err = s.Create(ctx, named("7KQ-42M-RX", alice))
	require.ErrorIs(t, err, store.ErrCaseCodeTaken)
}

func TestNamedReportsBelongToTheirReporter(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	mine, err := s.Create(ctx, named("AAA-AAA-AA", alice))
	require.NoError(t, err)
	_, err = s.Create(ctx, named("BBB-BBB-BB", bob))
	require.NoError(t, err)
	anon, err := s.Create(ctx, anonymous("CCC-CCC-CC"))
	require.NoError(t, err)

	list, err := s.ListByReporter(ctx, alice)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, mine, list[0].ID)
	require.Equal(t, "AAA-AAA-AA", list[0].CaseCode)

	_, err = s.GetOwned(ctx, mine, alice)
	require.NoError(t, err)
	_, err = s.GetOwned(ctx, mine, bob)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.GetOwned(ctx, anon, alice)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.GetOwned(ctx, "not-a-uuid", alice)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestReporterViewCarriesNoOfficerData(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	id, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)
	_, err = s.AddMessage(ctx, id, store.AuthorOfficer, grace, "Do you remember roughly how many names were on the list?")
	require.NoError(t, err)
	_, err = s.AddMessage(ctx, id, store.AuthorReporter, "", "About one page, maybe 30 names.")
	require.NoError(t, err)

	rv, err := s.GetForReporter(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "7KQ-42M-RX", rv.CaseCode)
	require.Len(t, rv.Thread, 2)
	require.Equal(t, store.AuthorOfficer, rv.Thread[0].Author)
	require.Empty(t, rv.Thread[0].OfficerUserID, "the reporter never learns which officer wrote")
	require.Equal(t, store.AuthorReporter, rv.Thread[1].Author)
}

func TestGetLoadsTheWholeCase(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	nc := anonymous("7KQ-42M-RX")
	nc.Attachments = []store.NewAttachment{{Filename: "attachment-1.txt", ContentType: "text/plain", Data: []byte("note")}}
	id, err := s.Create(ctx, nc)
	require.NoError(t, err)

	_, err = s.AddMessage(ctx, id, store.AuthorOfficer, grace, "Thank you.")
	require.NoError(t, err)
	_, err = s.AddNote(ctx, id, grace, "Print room has no badge release. Raise with Facilities.")
	require.NoError(t, err)
	require.NoError(t, s.SetAssignee(ctx, id, grace))
	f := domain.Factors{Information: []domain.InformationKind{domain.InfoHealth}, Recipient: domain.RecipientUnknownPeople,
		Viewed: domain.ViewedProbably, Mitigation: domain.MitigationPartly}
	_, err = s.AddAssessment(ctx, id, store.Assessment{Factors: f, Suggestion: domain.SuggestNotify,
		Decision: domain.DecisionNotReportable, Reason: "first look", DecidedBy: grace})
	require.NoError(t, err)
	latest, err := s.AddAssessment(ctx, id, store.Assessment{Factors: f, Suggestion: domain.SuggestNotify,
		Decision: domain.DecisionReportable, Reason: "Unattended for about 2 hours.", DecidedBy: grace})
	require.NoError(t, err)
	n, err := s.AddNotice(ctx, id, store.Notice{Recipient: domain.NoticeAffectedPeople, Label: "About 30 people", Method: "letter", DaysAllowed: 60})
	require.NoError(t, err)
	require.Equal(t, domain.NoticeNotSent, n.Status)

	c, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.KindAnonymous, c.Kind)
	require.Empty(t, c.ReporterUserID)
	require.Equal(t, domain.StatusNew, c.Status)
	require.Equal(t, details(), c.Details)
	require.Equal(t, grace, c.AssigneeUserID)
	require.Equal(t, day("2026-10-02"), c.DiscoveredOn)
	require.Len(t, c.Attachments, 1)
	require.Equal(t, int64(4), c.Attachments[0].Size)
	require.Len(t, c.Thread, 1)
	require.Equal(t, grace, c.Thread[0].OfficerUserID)
	require.Len(t, c.Notes, 1)
	require.NotNil(t, c.Assessment)
	require.Equal(t, latest.Reason, c.Assessment.Reason, "the latest assessment is the case's")
	require.Len(t, c.Notices, 1)

	att, data, err := s.Attachment(ctx, id, c.Attachments[0].ID)
	require.NoError(t, err)
	require.Equal(t, "text/plain", att.ContentType)
	require.Equal(t, "note", string(data))
	other, err := s.Create(ctx, anonymous("AAA-AAA-AA"))
	require.NoError(t, err)
	_, _, err = s.Attachment(ctx, other, c.Attachments[0].ID)
	require.ErrorIs(t, err, store.ErrNotFound, "an attachment is reached only through its own case")
}

func TestNoticesUpdateWithinTheirCase(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	id, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)
	other, err := s.Create(ctx, anonymous("AAA-AAA-AA"))
	require.NoError(t, err)
	n, err := s.AddNotice(ctx, id, store.Notice{Recipient: domain.NoticeRegulator, Method: "online form", DaysAllowed: 60})
	require.NoError(t, err)

	sent := day("2026-10-20")
	got, err := s.UpdateNotice(ctx, id, n.ID, domain.NoticeSent, &sent)
	require.NoError(t, err)
	require.Equal(t, domain.NoticeSent, got.Status)
	require.Equal(t, sent, *got.SentOn)

	_, err = s.UpdateNotice(ctx, other, n.ID, domain.NoticeDraft, nil)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestListFiltersCountsAndDeadlines(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	a, err := s.Create(ctx, anonymous("AAA-AAA-AA"))
	require.NoError(t, err)
	b, err := s.Create(ctx, named("BBB-BBB-BB", alice))
	require.NoError(t, err)
	_, err = s.Create(ctx, anonymous("CCC-CCC-CC"))
	require.NoError(t, err)
	require.NoError(t, s.SetStatus(ctx, b, domain.StatusNotificationDue))
	require.NoError(t, s.SetAssignee(ctx, b, grace))
	_, err = s.AddNotice(ctx, b, store.Notice{Recipient: domain.NoticeRegulator, DaysAllowed: 60})
	require.NoError(t, err)
	_, err = s.AddNotice(ctx, b, store.Notice{Recipient: domain.NoticeAffectedPeople, DaysAllowed: 30})
	require.NoError(t, err)
	done, err := s.AddNotice(ctx, b, store.Notice{Recipient: domain.NoticeMedia, DaysAllowed: 5})
	require.NoError(t, err)
	_, err = s.UpdateNotice(ctx, b, done.ID, domain.NoticeNotNeeded, nil)
	require.NoError(t, err)

	all, counts, err := s.List(ctx, store.Filter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	require.Equal(t, 2, counts[domain.StatusNew])
	require.Equal(t, 1, counts[domain.StatusNotificationDue])

	due, _, err := s.List(ctx, store.Filter{Statuses: []domain.Status{domain.StatusNotificationDue}})
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, b, due[0].ID)
	require.Equal(t, "Patient list left on a shared printer", due[0].Summary)
	require.Equal(t, "2026-11-01", due[0].NextDeadline, "the earliest deadline still open")

	mine, _, err := s.List(ctx, store.Filter{AssigneeUserID: grace})
	require.NoError(t, err)
	require.Len(t, mine, 1)

	news, _, err := s.List(ctx, store.Filter{Statuses: []domain.Status{domain.StatusNew}})
	require.NoError(t, err)
	require.Len(t, news, 2)
	require.Contains(t, []string{news[0].ID, news[1].ID}, a)
	require.Empty(t, news[0].NextDeadline)
}

func TestDiscoveryDateMovesEveryDeadline(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	id, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)
	_, err = s.AddNotice(ctx, id, store.Notice{Recipient: domain.NoticeRegulator, DaysAllowed: 60})
	require.NoError(t, err)
	require.NoError(t, s.SetDiscoveredOn(ctx, id, day("2026-09-30")))
	list, _, err := s.List(ctx, store.Filter{})
	require.NoError(t, err)
	require.Equal(t, "2026-11-29", list[0].NextDeadline)
}

func TestCloseRecordsTheOutcomeAndLocksTheCase(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	id, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)
	require.NoError(t, s.LockOpen(ctx, id))

	acts := []domain.CorrectiveAction{
		{Description: "Badge release on print room printers", PolicyID: "5b0b7f0e-7a51-4d38-9a0c-2f4b1d7f6a10"},
		{Description: "Remind staff about printouts"},
	}
	require.NoError(t, s.Close(ctx, id, domain.OutcomeSubstantiated, acts))
	c, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, domain.StatusClosed, c.Status)
	require.Equal(t, domain.OutcomeSubstantiated, c.Outcome)
	require.NotNil(t, c.ClosedAt)
	require.Equal(t, acts, c.CorrectiveActions)

	require.ErrorIs(t, s.LockOpen(ctx, id), store.ErrClosed)
	require.ErrorIs(t, s.LockOpen(ctx, "0b9f3d0c-1a2b-4c3d-8e4f-0000000000ff"), store.ErrNotFound)
	require.ErrorIs(t, s.LockOpen(ctx, "nope"), store.ErrNotFound)
}

func TestInTxRollsBackTogether(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	s := store.New(db)
	id, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)
	ob, err := outbox.New(outbox.WithTable("audit_outbox"))
	require.NoError(t, err)
	require.NoError(t, ob.Migrate(ctx, db))
	pub := store.NewOutboxPublisher(db, ob, "application/protobuf")

	boom := errors.New("boom")
	err = store.InTx(ctx, db, func(ctx context.Context) error {
		if _, err := s.AddNote(ctx, id, grace, "never kept"); err != nil {
			return err
		}
		if err := pub.Publish(ctx, "audit.audit", []byte("event")); err != nil {
			return err
		}
		return boom
	})
	require.ErrorIs(t, err, boom)
	c, err := s.Get(ctx, id)
	require.NoError(t, err)
	require.Empty(t, c.Notes)
	var queued int
	require.NoError(t, db.Querier().QueryRow(ctx, `SELECT count(*) FROM audit_outbox`).Scan(&queued))
	require.Zero(t, queued, "the audit event rolls back with the change")

	require.NoError(t, store.InTx(ctx, db, func(ctx context.Context) error {
		if _, err := s.AddNote(ctx, id, grace, "kept"); err != nil {
			return err
		}
		return pub.Publish(ctx, "audit.audit", []byte("event"))
	}))
	require.NoError(t, db.Querier().QueryRow(ctx, `SELECT count(*) FROM audit_outbox`).Scan(&queued))
	require.Equal(t, 1, queued)
}
