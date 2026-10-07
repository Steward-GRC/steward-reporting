// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
)

type categoryJSON struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// SeedSettings stores s unless settings are already stored, so the seed only
// ever applies to a fresh database.
func (s *Store) SeedSettings(ctx context.Context, st domain.Settings) (seeded bool, err error) {
	cats, err := categoriesJSON(st.Categories)
	if err != nil {
		return false, err
	}
	tag, err := s.q(ctx).Exec(ctx, `INSERT INTO compliance_settings (officer_groups, public_link_enabled, retention_days, intake_categories)
		VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`, nonNil(st.OfficerGroups), st.PublicLink, st.RetentionDays, cats)
	if err != nil {
		return false, fmt.Errorf("store: seed settings: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Settings loads the stored settings; ErrNotFound before they are seeded.
func (s *Store) Settings(ctx context.Context) (domain.Settings, error) {
	var st domain.Settings
	var cats []byte
	var by *string
	err := s.q(ctx).QueryRow(ctx, `SELECT officer_groups, public_link_enabled, retention_days, intake_categories,
			updated_at, updated_by_user_id
		FROM compliance_settings`).Scan(&st.OfficerGroups, &st.PublicLink, &st.RetentionDays, &cats, &st.UpdatedAt, &by)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Settings{}, ErrNotFound
	}
	if err != nil {
		return domain.Settings{}, fmt.Errorf("store: load settings: %w", err)
	}
	var in []categoryJSON
	if err := json.Unmarshal(cats, &in); err != nil {
		return domain.Settings{}, fmt.Errorf("store: decode intake categories: %w", err)
	}
	for _, c := range in {
		st.Categories = append(st.Categories, domain.Category{Key: c.Key, Label: c.Label})
	}
	if by != nil {
		st.UpdatedBy = *by
	}
	return st, nil
}

// SaveSettings replaces the stored settings, recording who saved them.
func (s *Store) SaveSettings(ctx context.Context, st domain.Settings, by string) error {
	cats, err := categoriesJSON(st.Categories)
	if err != nil {
		return err
	}
	_, err = s.q(ctx).Exec(ctx, `INSERT INTO compliance_settings (officer_groups, public_link_enabled, retention_days,
			intake_categories, updated_at, updated_by_user_id)
		VALUES ($1, $2, $3, $4, now(), $5)
		ON CONFLICT (id) DO UPDATE SET officer_groups = EXCLUDED.officer_groups,
			public_link_enabled = EXCLUDED.public_link_enabled, retention_days = EXCLUDED.retention_days,
			intake_categories = EXCLUDED.intake_categories, updated_at = EXCLUDED.updated_at,
			updated_by_user_id = EXCLUDED.updated_by_user_id`,
		nonNil(st.OfficerGroups), st.PublicLink, st.RetentionDays, cats, by)
	if err != nil {
		return fmt.Errorf("store: save settings: %w", err)
	}
	return nil
}

func categoriesJSON(in []domain.Category) ([]byte, error) {
	out := make([]categoryJSON, len(in))
	for i, c := range in {
		out[i] = categoryJSON{Key: c.Key, Label: c.Label}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("store: encode intake categories: %w", err)
	}
	return b, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
