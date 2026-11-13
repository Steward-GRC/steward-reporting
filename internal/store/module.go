// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrHeld means a legal hold stops a hard reset.
var ErrHeld = errors.New("store: a case is under a legal hold")

// hardResetLock names the advisory lock that serialises hard resets and
// keeps a hold placement out of a reset's window.
const hardResetLock = "reporting.hard_reset"

// ModuleEnabled reports whether the Compliance module is on. A database with
// no settings row yet, before the start-up seed, is on.
func (s *Store) ModuleEnabled(ctx context.Context) (bool, error) {
	var on bool
	err := s.q(ctx).QueryRow(ctx, `SELECT module_enabled FROM compliance_settings`).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: read module switch: %w", err)
	}
	return on, nil
}

// SetModuleEnabled turns the module on or off, recording who did. It changes
// no case; ErrNotFound before the settings are seeded.
func (s *Store) SetModuleEnabled(ctx context.Context, enabled bool, by string) error {
	tag, err := s.q(ctx).Exec(ctx, `UPDATE compliance_settings SET module_enabled = $1, updated_at = now(),
		updated_by_user_id = $2`, enabled, by)
	if err != nil {
		return fmt.Errorf("store: set module switch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// HoldCount is how many cases are under a legal hold.
func (s *Store) HoldCount(ctx context.Context) (int64, error) {
	var n int64
	if err := s.q(ctx).QueryRow(ctx, `SELECT count(*) FROM case_legal_holds`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count legal holds: %w", err)
	}
	return n, nil
}

// HardResetCases deletes every case, and with it every attachment, message,
// note, assessment, notice and corrective action, and returns how many cases
// went. It refuses with ErrHeld, deleting nothing, when any case is under a
// legal hold. It takes the hard reset advisory lock and rechecks the holds
// inside the transaction it runs in, so call it in InTx with the tombstone's
// audit event. The settings and the module switch are kept.
func (s *Store) HardResetCases(ctx context.Context) (int64, error) {
	got, err := s.TryLock(ctx, hardResetLock)
	if err != nil {
		return 0, err
	}
	if !got {
		return 0, fmt.Errorf("store: another hard reset is running")
	}
	held, err := s.HoldCount(ctx)
	if err != nil {
		return 0, err
	}
	if held > 0 {
		return 0, ErrHeld
	}
	tag, err := s.q(ctx).Exec(ctx, `DELETE FROM cases`)
	if err != nil {
		return 0, fmt.Errorf("store: hard reset cases: %w", err)
	}
	return tag.RowsAffected(), nil
}
