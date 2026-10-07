// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Hold is a legal hold on a case.
type Hold struct {
	CaseID   string
	PlacedBy string
	PlacedAt time.Time
}

// PlaceHold puts a case under a legal hold and returns the hold. A case
// already held keeps its first hold, and placed reports false.
func (s *Store) PlaceHold(ctx context.Context, caseID, by string) (h Hold, placed bool, err error) {
	if !validID(caseID) {
		return Hold{}, false, ErrNotFound
	}
	tag, err := s.q(ctx).Exec(ctx, `INSERT INTO case_legal_holds (case_id, placed_by_user_id) VALUES ($1, $2)
		ON CONFLICT (case_id) DO NOTHING`, caseID, by)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return Hold{}, false, ErrNotFound
	}
	if err != nil {
		return Hold{}, false, fmt.Errorf("store: place legal hold: %w", err)
	}
	h = Hold{CaseID: caseID}
	if err := s.q(ctx).QueryRow(ctx, `SELECT placed_by_user_id, placed_at FROM case_legal_holds WHERE case_id = $1`, caseID).
		Scan(&h.PlacedBy, &h.PlacedAt); err != nil {
		return Hold{}, false, fmt.Errorf("store: read legal hold: %w", err)
	}
	return h, tag.RowsAffected() == 1, nil
}

// ReleaseHold lifts a case's legal hold; ErrNotFound when it has none.
func (s *Store) ReleaseHold(ctx context.Context, caseID string) error {
	if !validID(caseID) {
		return ErrNotFound
	}
	tag, err := s.q(ctx).Exec(ctx, `DELETE FROM case_legal_holds WHERE case_id = $1`, caseID)
	if err != nil {
		return fmt.Errorf("store: release legal hold: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Holds lists every legal hold, newest first.
func (s *Store) Holds(ctx context.Context) ([]Hold, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT case_id, placed_by_user_id, placed_at FROM case_legal_holds
		ORDER BY placed_at DESC, case_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list legal holds: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Hold, error) {
		var h Hold
		return h, r.Scan(&h.CaseID, &h.PlacedBy, &h.PlacedAt)
	})
}

// TryLock takes the transaction-scoped advisory lock named name, and reports
// false when another transaction holds it. It must run inside InTx.
func (s *Store) TryLock(ctx context.Context, name string) (bool, error) {
	var got bool
	if err := s.q(ctx).QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`, name).Scan(&got); err != nil {
		return false, fmt.Errorf("store: advisory lock: %w", err)
	}
	return got, nil
}

// PurgeClosedBefore deletes up to limit closed cases that closed before
// before and are under no legal hold, with every row that belongs to them,
// and returns their ids. Rows another transaction holds are skipped.
func (s *Store) PurgeClosedBefore(ctx context.Context, before time.Time, limit int) ([]string, error) {
	rows, err := s.q(ctx).Query(ctx, `DELETE FROM cases WHERE id IN (
			SELECT c.id FROM cases c
			WHERE c.status = 'closed' AND c.closed_at < $1
				AND NOT EXISTS (SELECT 1 FROM case_legal_holds h WHERE h.case_id = c.id)
			ORDER BY c.closed_at, c.id LIMIT $2 FOR UPDATE SKIP LOCKED)
		RETURNING id`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("store: purge closed cases: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("store: purge closed cases: %w", err)
	}
	return ids, nil
}
