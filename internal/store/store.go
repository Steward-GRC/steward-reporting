// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package store holds the reporting service's Postgres queries. Every call
// joins the transaction InTx put in its context, if any.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	postgres "github.com/Bugs5382/go-postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
)

// Errors the store returns.
var (
	// ErrNotFound means no row matched; a malformed id matches nothing.
	ErrNotFound = errors.New("store: not found")
	// ErrCaseCodeTaken means the case code is already in use.
	ErrCaseCodeTaken = errors.New("store: case code taken")
	// ErrClosed means the case is closed.
	ErrClosed = errors.New("store: case closed")
)

// Author says who wrote a thread message.
type Author string

// The authors.
const (
	AuthorReporter Author = "reporter"
	AuthorOfficer  Author = "officer"
)

// NewAttachment is a stripped attachment to store.
type NewAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// NewCase is a report to file.
type NewCase struct {
	Kind     domain.Kind
	CaseCode string
	// ReporterUserID is set on a named case only.
	ReporterUserID string
	// PassphraseHash is set on an anonymous case only.
	PassphraseHash string
	Details        domain.Details
	DiscoveredOn   time.Time
	Attachments    []NewAttachment
}

// Attachment describes a stored attachment.
type Attachment struct {
	ID          string
	Filename    string
	ContentType string
	Size        int64
}

// Message is one thread message.
type Message struct {
	ID     string
	Author Author
	// OfficerUserID is empty on reporter messages and in a reporter's view.
	OfficerUserID string
	Body          string
	CreatedAt     time.Time
}

// Note is an internal note.
type Note struct {
	ID           string
	AuthorUserID string
	Body         string
	CreatedAt    time.Time
}

// Assessment is a recorded risk assessment.
type Assessment struct {
	Factors    domain.Factors
	Suggestion domain.Suggestion
	Decision   domain.Decision
	Reason     string
	DecidedBy  string
	DecidedAt  time.Time
}

// Notice is a notice a case owes.
type Notice struct {
	ID          string
	Recipient   domain.NoticeRecipient
	Label       string
	Method      string
	DaysAllowed int
	Status      domain.NoticeStatus
	SentOn      *time.Time
}

// Case is the whole case, for an officer.
type Case struct {
	ID                string
	CaseCode          string
	Kind              domain.Kind
	Status            domain.Status
	Details           domain.Details
	ReporterUserID    string
	AssigneeUserID    string
	ReceivedAt        time.Time
	DiscoveredOn      time.Time
	Outcome           domain.Outcome
	ClosedAt          *time.Time
	Attachments       []Attachment
	Thread            []Message
	Notes             []Note
	Assessment        *Assessment
	Notices           []Notice
	CorrectiveActions []domain.CorrectiveAction
}

// ReporterCase is what a reporter may see: no notes, assessment, notices,
// assignee or officer ids are ever loaded into it.
type ReporterCase struct {
	ID         string
	CaseCode   string
	Status     domain.Status
	Details    domain.Details
	ReceivedAt time.Time
	Thread     []Message
}

// Summary is one row of the case queue.
type Summary struct {
	ID             string
	CaseCode       string
	Kind           domain.Kind
	Status         domain.Status
	Summary        string
	AssigneeUserID string
	ReceivedAt     time.Time
	// NextDeadline is the earliest deadline of a notice not yet sent or
	// marked not needed, YYYY-MM-DD; empty for none.
	NextDeadline string
}

// Filter narrows the queue.
type Filter struct {
	Statuses       []domain.Status
	AssigneeUserID string
}

// Store is the reporting store.
type Store struct{ db *postgres.DB }

// New returns a store on db.
func New(db *postgres.DB) *Store { return &Store{db: db} }

func (s *Store) q(ctx context.Context) postgres.Querier { return querier(ctx, s.db) }

func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

func kindsToText(k []domain.InformationKind) []string {
	out := make([]string, len(k))
	for i, v := range k {
		out[i] = string(v)
	}
	return out
}

func textToKinds(s []string) []domain.InformationKind {
	out := make([]domain.InformationKind, len(s))
	for i, v := range s {
		out[i] = domain.InformationKind(v)
	}
	return out
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Create files a case with its attachments and returns its id.
func (s *Store) Create(ctx context.Context, nc NewCase) (string, error) {
	var id string
	err := InTx(ctx, s.db, func(ctx context.Context) error {
		q := s.q(ctx)
		err := q.QueryRow(ctx, `INSERT INTO cases (case_code, kind, reporter_user_id, passphrase_hash, what_happened,
				occurred, location, information_kinds, still_happening, discovered_on)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			nc.CaseCode, string(nc.Kind), nullable(nc.ReporterUserID), nullable(nc.PassphraseHash), nc.Details.WhatHappened,
			nc.Details.Occurred, nc.Details.Location, kindsToText(nc.Details.InformationKinds), string(nc.Details.StillHappening),
			nc.DiscoveredOn).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "cases_case_code_key" {
			return ErrCaseCodeTaken
		}
		if err != nil {
			return fmt.Errorf("store: insert case: %w", err)
		}
		for i, a := range nc.Attachments {
			if _, err := q.Exec(ctx, `INSERT INTO case_attachments (case_id, position, filename, content_type, size_bytes, data)
				VALUES ($1, $2, $3, $4, $5, $6)`, id, i+1, a.Filename, a.ContentType, len(a.Data), a.Data); err != nil {
				return fmt.Errorf("store: insert attachment: %w", err)
			}
		}
		return nil
	})
	return id, err
}

// FindAnonymous returns the id and passphrase hash of the anonymous case
// with this code.
func (s *Store) FindAnonymous(ctx context.Context, code string) (id, passphraseHash string, err error) {
	err = s.q(ctx).QueryRow(ctx, `SELECT id, passphrase_hash FROM cases WHERE case_code = $1 AND kind = 'anonymous'`, code).
		Scan(&id, &passphraseHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("store: find anonymous case: %w", err)
	}
	return id, passphraseHash, nil
}

const reporterCols = `id, case_code, status, what_happened, occurred, location, information_kinds, still_happening, received_at`

func scanReporter(row pgx.Row) (ReporterCase, error) {
	var rc ReporterCase
	var status, still string
	var kinds []string
	err := row.Scan(&rc.ID, &rc.CaseCode, &status, &rc.Details.WhatHappened, &rc.Details.Occurred, &rc.Details.Location,
		&kinds, &still, &rc.ReceivedAt)
	rc.Status, rc.Details.StillHappening, rc.Details.InformationKinds = domain.Status(status), domain.Answer(still), textToKinds(kinds)
	return rc, err
}

// GetForReporter loads the reporter's view of a case.
func (s *Store) GetForReporter(ctx context.Context, id string) (ReporterCase, error) {
	if !validID(id) {
		return ReporterCase{}, ErrNotFound
	}
	rc, err := scanReporter(s.q(ctx).QueryRow(ctx, `SELECT `+reporterCols+` FROM cases WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ReporterCase{}, ErrNotFound
	}
	if err != nil {
		return ReporterCase{}, fmt.Errorf("store: get case for reporter: %w", err)
	}
	thread, err := s.thread(ctx, id, false)
	if err != nil {
		return ReporterCase{}, err
	}
	rc.Thread = thread
	return rc, nil
}

// GetOwned loads the reporter's view of a named case, only for its reporter.
func (s *Store) GetOwned(ctx context.Context, id, reporterUserID string) (ReporterCase, error) {
	if !validID(id) {
		return ReporterCase{}, ErrNotFound
	}
	var owned bool
	err := s.q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cases WHERE id = $1 AND kind = 'named' AND reporter_user_id = $2)`,
		id, reporterUserID).Scan(&owned)
	if err != nil {
		return ReporterCase{}, fmt.Errorf("store: check owner: %w", err)
	}
	if !owned {
		return ReporterCase{}, ErrNotFound
	}
	return s.GetForReporter(ctx, id)
}

// ListByReporter lists a reporter's own named cases, newest first.
func (s *Store) ListByReporter(ctx context.Context, reporterUserID string) ([]ReporterCase, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT `+reporterCols+` FROM cases
		WHERE kind = 'named' AND reporter_user_id = $1 ORDER BY received_at DESC, id`, reporterUserID)
	if err != nil {
		return nil, fmt.Errorf("store: list own cases: %w", err)
	}
	var out []ReporterCase
	for rows.Next() {
		rc, err := scanReporter(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("store: scan own case: %w", err)
		}
		out = append(out, rc)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list own cases: %w", err)
	}
	for i := range out {
		if out[i].Thread, err = s.thread(ctx, out[i].ID, false); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) thread(ctx context.Context, caseID string, officerView bool) ([]Message, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT id, author, coalesce(officer_user_id, ''), body, created_at
		FROM case_messages WHERE case_id = $1 ORDER BY created_at, id`, caseID)
	if err != nil {
		return nil, fmt.Errorf("store: load thread: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) {
		var m Message
		var author string
		err := r.Scan(&m.ID, &author, &m.OfficerUserID, &m.Body, &m.CreatedAt)
		m.Author = Author(author)
		if !officerView {
			m.OfficerUserID = ""
		}
		return m, err
	})
}

// Get loads the whole case for an officer.
func (s *Store) Get(ctx context.Context, id string) (Case, error) {
	if !validID(id) {
		return Case{}, ErrNotFound
	}
	q := s.q(ctx)
	var c Case
	var kind, status, still string
	var outcome *string
	var kinds []string
	err := q.QueryRow(ctx, `SELECT id, case_code, kind, status, what_happened, occurred, location, information_kinds,
			still_happening, coalesce(reporter_user_id, ''), coalesce(assignee_user_id, ''), received_at, discovered_on,
			outcome, closed_at
		FROM cases WHERE id = $1`, id).Scan(&c.ID, &c.CaseCode, &kind, &status, &c.Details.WhatHappened, &c.Details.Occurred,
		&c.Details.Location, &kinds, &still, &c.ReporterUserID, &c.AssigneeUserID, &c.ReceivedAt, &c.DiscoveredOn,
		&outcome, &c.ClosedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Case{}, ErrNotFound
	}
	if err != nil {
		return Case{}, fmt.Errorf("store: get case: %w", err)
	}
	c.Kind, c.Status, c.Details.StillHappening, c.Details.InformationKinds = domain.Kind(kind), domain.Status(status),
		domain.Answer(still), textToKinds(kinds)
	if outcome != nil {
		c.Outcome = domain.Outcome(*outcome)
	}
	if c.Attachments, err = s.attachments(ctx, id); err != nil {
		return Case{}, err
	}
	if c.Thread, err = s.thread(ctx, id, true); err != nil {
		return Case{}, err
	}
	if c.Notes, err = s.notes(ctx, id); err != nil {
		return Case{}, err
	}
	if c.Assessment, err = s.latestAssessment(ctx, id); err != nil {
		return Case{}, err
	}
	if c.Notices, err = s.notices(ctx, id); err != nil {
		return Case{}, err
	}
	if c.CorrectiveActions, err = s.correctiveActions(ctx, id); err != nil {
		return Case{}, err
	}
	return c, nil
}

func (s *Store) attachments(ctx context.Context, caseID string) ([]Attachment, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT id, filename, content_type, size_bytes FROM case_attachments
		WHERE case_id = $1 ORDER BY position`, caseID)
	if err != nil {
		return nil, fmt.Errorf("store: load attachments: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Attachment, error) {
		var a Attachment
		return a, r.Scan(&a.ID, &a.Filename, &a.ContentType, &a.Size)
	})
}

func (s *Store) notes(ctx context.Context, caseID string) ([]Note, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT id, author_user_id, body, created_at FROM case_notes
		WHERE case_id = $1 ORDER BY created_at, id`, caseID)
	if err != nil {
		return nil, fmt.Errorf("store: load notes: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Note, error) {
		var n Note
		return n, r.Scan(&n.ID, &n.AuthorUserID, &n.Body, &n.CreatedAt)
	})
}

func (s *Store) latestAssessment(ctx context.Context, caseID string) (*Assessment, error) {
	var a Assessment
	var kinds []string
	var recipient, viewed, mitigation, suggestion, decision string
	err := s.q(ctx).QueryRow(ctx, `SELECT information_kinds, recipient, viewed, mitigation, suggestion, decision, reason,
			decided_by_user_id, decided_at
		FROM case_assessments WHERE case_id = $1 ORDER BY decided_at DESC, id DESC LIMIT 1`, caseID).
		Scan(&kinds, &recipient, &viewed, &mitigation, &suggestion, &decision, &a.Reason, &a.DecidedBy, &a.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load assessment: %w", err)
	}
	a.Factors = domain.Factors{Information: textToKinds(kinds), Recipient: domain.Recipient(recipient),
		Viewed: domain.Viewed(viewed), Mitigation: domain.Mitigation(mitigation)}
	a.Suggestion, a.Decision = domain.Suggestion(suggestion), domain.Decision(decision)
	return &a, nil
}

const noticeCols = `id, recipient, label, method, days_allowed, status, sent_on`

func scanNotice(row pgx.Row) (Notice, error) {
	var n Notice
	var recipient, status string
	err := row.Scan(&n.ID, &recipient, &n.Label, &n.Method, &n.DaysAllowed, &status, &n.SentOn)
	n.Recipient, n.Status = domain.NoticeRecipient(recipient), domain.NoticeStatus(status)
	return n, err
}

func (s *Store) notices(ctx context.Context, caseID string) ([]Notice, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT `+noticeCols+` FROM case_notices WHERE case_id = $1 ORDER BY created_at, id`, caseID)
	if err != nil {
		return nil, fmt.Errorf("store: load notices: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notice, error) { return scanNotice(r) })
}

func (s *Store) correctiveActions(ctx context.Context, caseID string) ([]domain.CorrectiveAction, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT description, coalesce(policy_id::text, '') FROM case_corrective_actions
		WHERE case_id = $1 ORDER BY position`, caseID)
	if err != nil {
		return nil, fmt.Errorf("store: load corrective actions: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.CorrectiveAction, error) {
		var a domain.CorrectiveAction
		return a, r.Scan(&a.Description, &a.PolicyID)
	})
}

// Attachment returns one attachment of a case with its bytes.
func (s *Store) Attachment(ctx context.Context, caseID, attachmentID string) (Attachment, []byte, error) {
	if !validID(caseID) || !validID(attachmentID) {
		return Attachment{}, nil, ErrNotFound
	}
	var a Attachment
	var data []byte
	err := s.q(ctx).QueryRow(ctx, `SELECT id, filename, content_type, size_bytes, data FROM case_attachments
		WHERE case_id = $1 AND id = $2`, caseID, attachmentID).Scan(&a.ID, &a.Filename, &a.ContentType, &a.Size, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, nil, ErrNotFound
	}
	if err != nil {
		return Attachment{}, nil, fmt.Errorf("store: get attachment: %w", err)
	}
	return a, data, nil
}

// List returns the queue, newest first, and the count per status over every
// case.
func (s *Store) List(ctx context.Context, f Filter) ([]Summary, map[domain.Status]int, error) {
	statuses := make([]string, len(f.Statuses))
	for i, st := range f.Statuses {
		statuses[i] = string(st)
	}
	q := s.q(ctx)
	rows, err := q.Query(ctx, `SELECT c.id, c.case_code, c.kind, c.status, c.what_happened, coalesce(c.assignee_user_id, ''),
			c.received_at,
			coalesce(to_char((SELECT min(c.discovered_on + n.days_allowed) FROM case_notices n
				WHERE n.case_id = c.id AND n.status IN ('not_sent', 'draft')), 'YYYY-MM-DD'), '')
		FROM cases c
		WHERE (cardinality($1::text[]) = 0 OR c.status = ANY($1))
		  AND ($2 = '' OR c.assignee_user_id = $2)
		ORDER BY c.received_at DESC, c.id`, statuses, f.AssigneeUserID)
	if err != nil {
		return nil, nil, fmt.Errorf("store: list cases: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Summary, error) {
		var sm Summary
		var kind, status, what string
		err := r.Scan(&sm.ID, &sm.CaseCode, &kind, &status, &what, &sm.AssigneeUserID, &sm.ReceivedAt, &sm.NextDeadline)
		sm.Kind, sm.Status, sm.Summary = domain.Kind(kind), domain.Status(status), domain.Summary(what)
		return sm, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("store: list cases: %w", err)
	}
	counts := map[domain.Status]int{}
	rows, err = q.Query(ctx, `SELECT status, count(*) FROM cases GROUP BY status`)
	if err != nil {
		return nil, nil, fmt.Errorf("store: count cases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, nil, fmt.Errorf("store: count cases: %w", err)
		}
		counts[domain.Status(st)] = n
	}
	return list, counts, rows.Err()
}

// LockOpen locks an open case's row for the rest of the transaction. It
// returns ErrClosed for a closed case. Call it inside InTx before a change.
func (s *Store) LockOpen(ctx context.Context, id string) error {
	if !validID(id) {
		return ErrNotFound
	}
	var status string
	err := s.q(ctx).QueryRow(ctx, `SELECT status FROM cases WHERE id = $1 FOR UPDATE`, id).Scan(&status)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("store: lock case: %w", err)
	case domain.Status(status) == domain.StatusClosed:
		return ErrClosed
	}
	return nil
}

// AddMessage adds a thread message; officerUserID is empty for the
// reporter.
func (s *Store) AddMessage(ctx context.Context, caseID string, author Author, officerUserID, body string) (Message, error) {
	m := Message{Author: author, OfficerUserID: officerUserID, Body: body}
	err := s.q(ctx).QueryRow(ctx, `INSERT INTO case_messages (case_id, author, officer_user_id, body) VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`, caseID, string(author), nullable(officerUserID), body).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return Message{}, fmt.Errorf("store: add message: %w", err)
	}
	return m, nil
}

// AddNote adds an internal note.
func (s *Store) AddNote(ctx context.Context, caseID, authorUserID, body string) (Note, error) {
	n := Note{AuthorUserID: authorUserID, Body: body}
	err := s.q(ctx).QueryRow(ctx, `INSERT INTO case_notes (case_id, author_user_id, body) VALUES ($1, $2, $3)
		RETURNING id, created_at`, caseID, authorUserID, body).Scan(&n.ID, &n.CreatedAt)
	if err != nil {
		return Note{}, fmt.Errorf("store: add note: %w", err)
	}
	return n, nil
}

func (s *Store) update(ctx context.Context, op, sql string, args ...any) error {
	tag, err := s.q(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("store: %s: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAssignee sets the assignee; empty clears it.
func (s *Store) SetAssignee(ctx context.Context, id, userID string) error {
	return s.update(ctx, "set assignee", `UPDATE cases SET assignee_user_id = $2 WHERE id = $1`, id, nullable(userID))
}

// SetStatus moves a case.
func (s *Store) SetStatus(ctx context.Context, id string, st domain.Status) error {
	return s.update(ctx, "set status", `UPDATE cases SET status = $2 WHERE id = $1`, id, string(st))
}

// SetDiscoveredOn sets the discovery date.
func (s *Store) SetDiscoveredOn(ctx context.Context, id string, d time.Time) error {
	return s.update(ctx, "set discovery date", `UPDATE cases SET discovered_on = $2 WHERE id = $1`, id, d)
}

// AddAssessment records an assessment.
func (s *Store) AddAssessment(ctx context.Context, caseID string, a Assessment) (Assessment, error) {
	err := s.q(ctx).QueryRow(ctx, `INSERT INTO case_assessments (case_id, information_kinds, recipient, viewed, mitigation,
			suggestion, decision, reason, decided_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING decided_at`,
		caseID, kindsToText(a.Factors.Information), string(a.Factors.Recipient), string(a.Factors.Viewed),
		string(a.Factors.Mitigation), string(a.Suggestion), string(a.Decision), a.Reason, a.DecidedBy).Scan(&a.DecidedAt)
	if err != nil {
		return Assessment{}, fmt.Errorf("store: add assessment: %w", err)
	}
	return a, nil
}

// AddNotice adds a notice, not yet sent.
func (s *Store) AddNotice(ctx context.Context, caseID string, n Notice) (Notice, error) {
	got, err := scanNotice(s.q(ctx).QueryRow(ctx, `INSERT INTO case_notices (case_id, recipient, label, method, days_allowed)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+noticeCols, caseID, string(n.Recipient), n.Label, n.Method, n.DaysAllowed))
	if err != nil {
		return Notice{}, fmt.Errorf("store: add notice: %w", err)
	}
	return got, nil
}

// UpdateNotice sets a notice's status and sent date.
func (s *Store) UpdateNotice(ctx context.Context, caseID, noticeID string, st domain.NoticeStatus, sentOn *time.Time) (Notice, error) {
	if !validID(caseID) || !validID(noticeID) {
		return Notice{}, ErrNotFound
	}
	n, err := scanNotice(s.q(ctx).QueryRow(ctx, `UPDATE case_notices SET status = $3, sent_on = $4
		WHERE case_id = $1 AND id = $2 RETURNING `+noticeCols, caseID, noticeID, string(st), sentOn))
	if errors.Is(err, pgx.ErrNoRows) {
		return Notice{}, ErrNotFound
	}
	if err != nil {
		return Notice{}, fmt.Errorf("store: update notice: %w", err)
	}
	return n, nil
}

// Close closes a case with its outcome and corrective actions.
func (s *Store) Close(ctx context.Context, id string, o domain.Outcome, actions []domain.CorrectiveAction) error {
	return InTx(ctx, s.db, func(ctx context.Context) error {
		if err := s.update(ctx, "close case", `UPDATE cases SET status = 'closed', outcome = $2, closed_at = now() WHERE id = $1`,
			id, string(o)); err != nil {
			return err
		}
		for i, a := range actions {
			if _, err := s.q(ctx).Exec(ctx, `INSERT INTO case_corrective_actions (case_id, position, description, policy_id)
				VALUES ($1, $2, $3, $4)`, id, i+1, a.Description, nullable(a.PolicyID)); err != nil {
				return fmt.Errorf("store: add corrective action: %w", err)
			}
		}
		return nil
	})
}
