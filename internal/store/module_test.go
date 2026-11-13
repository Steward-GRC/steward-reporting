// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

func TestModuleIsOnUntilSwitchedOff(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))

	on, err := s.ModuleEnabled(ctx)
	require.NoError(t, err)
	require.True(t, on, "a database with no settings row yet is on")

	_, err = s.SeedSettings(ctx, domain.DefaultSettings())
	require.NoError(t, err)
	on, err = s.ModuleEnabled(ctx)
	require.NoError(t, err)
	require.True(t, on, "the seed leaves the module on")

	require.NoError(t, s.SetModuleEnabled(ctx, false, grace))
	on, err = s.ModuleEnabled(ctx)
	require.NoError(t, err)
	require.False(t, on)

	st, err := s.Settings(ctx)
	require.NoError(t, err)
	require.Equal(t, grace, st.UpdatedBy)
	require.False(t, st.ModuleEnabled)
}

func TestSavingSettingsNeverFlipsTheModule(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	_, err := s.SeedSettings(ctx, domain.DefaultSettings())
	require.NoError(t, err)
	require.NoError(t, s.SetModuleEnabled(ctx, false, grace))

	require.NoError(t, s.SaveSettings(ctx, domain.DefaultSettings(), bob))
	on, err := s.ModuleEnabled(ctx)
	require.NoError(t, err)
	require.False(t, on, "SaveSettings replaces the settings, not the module switch")
}

func TestHardResetDeletesEveryCaseAndItsRows(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	s := store.New(db)
	_, err := s.SeedSettings(ctx, domain.DefaultSettings())
	require.NoError(t, err)
	a, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)
	_, err = s.Create(ctx, named("N22-222-22", alice))
	require.NoError(t, err)
	_, err = s.AddNote(ctx, a, grace, "a note")
	require.NoError(t, err)

	n, err := s.HardResetCases(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, n)

	for _, table := range []string{"cases", "case_notes", "case_messages", "case_attachments", "case_assessments", "case_notices", "case_corrective_actions"} {
		var c int
		require.NoError(t, db.Querier().QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&c))
		require.Zero(t, c, table)
	}
	_, err = s.Settings(ctx)
	require.NoError(t, err, "the settings row survives a reset")
}

func TestHardResetRefusesWholeWhenAnyCaseIsHeld(t *testing.T) {
	ctx := context.Background()
	s := store.New(newTestDB(t))
	held, err := s.Create(ctx, anonymous("7KQ-42M-RX"))
	require.NoError(t, err)
	_, err = s.Create(ctx, named("N22-222-22", alice))
	require.NoError(t, err)
	_, _, err = s.PlaceHold(ctx, held, grace)
	require.NoError(t, err)

	hc, err := s.HoldCount(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, hc)

	n, err := s.HardResetCases(ctx)
	require.ErrorIs(t, err, store.ErrHeld)
	require.Zero(t, n)

	list, _, err := s.List(ctx, store.Filter{})
	require.NoError(t, err)
	require.Len(t, list, 2, "nothing was deleted, the unheld case included")
}
