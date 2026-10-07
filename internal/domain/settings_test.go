// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Bugs5382/go-apperr/apperrgrpc"
	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

func invalidField(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	info, ok := apperrgrpc.FromError(errcodes.Error(t.Context(), err))
	require.True(t, ok)
	require.Equal(t, errcodes.CodeInvalid, info.Code)
	return info.Metadata["field"]
}

func TestDefaultSettingsSeedTheOfficerGroups(t *testing.T) {
	s := domain.DefaultSettings([]string{"Privacy Officers"})
	require.Equal(t, []string{"Privacy Officers"}, s.OfficerGroups)
	require.True(t, s.PublicLink, "the public link starts on")
	require.Equal(t, domain.DefaultRetentionDays, s.RetentionDays)
	require.Empty(t, s.Categories)
}

func TestSettingsNormalizeTrimsAndDedupes(t *testing.T) {
	s, err := domain.Settings{
		OfficerGroups: []string{" Privacy Officers ", "privacy officers", "", "9c1d4e2a-0000-4000-8000-000000000001"},
		PublicLink:    true, RetentionDays: 365,
		Categories: []domain.Category{{Key: "printing", Label: " Printed records "}, {Key: "email", Label: "Email sent to the wrong person"}},
	}.Normalize()
	require.NoError(t, err)
	require.Equal(t, []string{"Privacy Officers", "9c1d4e2a-0000-4000-8000-000000000001"}, s.OfficerGroups)
	require.Equal(t, "Printed records", s.Categories[0].Label)
}

func TestSettingsNormalizeRefusesBadValues(t *testing.T) {
	ok := func() domain.Settings {
		return domain.Settings{RetentionDays: 365, Categories: []domain.Category{{Key: "printing", Label: "Printed records"}}}
	}
	cases := map[string]func(s *domain.Settings){
		"retention_days": func(s *domain.Settings) { s.RetentionDays = domain.MinRetentionDays - 1 },
		"officer_groups": func(s *domain.Settings) { s.OfficerGroups = []string{strings.Repeat("g", domain.MaxShortFieldLen+1)} },
	}
	for field, mutate := range cases {
		s := ok()
		mutate(&s)
		_, err := s.Normalize()
		require.Equal(t, field, invalidField(t, err), field)
	}
	s := ok()
	s.RetentionDays = domain.MaxRetentionDays + 1
	_, err := s.Normalize()
	require.Equal(t, "retention_days", invalidField(t, err))
	for _, cats := range [][]domain.Category{
		{{Key: "Printing", Label: "x"}},
		{{Key: "printing", Label: ""}},
		{{Key: "printing", Label: "a"}, {Key: "printing", Label: "b"}},
		{{Key: strings.Repeat("k", domain.MaxCategoryKeyLen+1), Label: "x"}},
	} {
		s := ok()
		s.Categories = cats
		_, err := s.Normalize()
		require.Equal(t, "intake_categories", invalidField(t, err), "%v", cats)
	}
	many := ok()
	many.Categories = nil
	for i := range domain.MaxCategories + 1 {
		many.Categories = append(many.Categories, domain.Category{Key: fmt.Sprintf("c%02d", i), Label: "x"})
	}
	_, err = many.Normalize()
	require.Equal(t, "intake_categories", invalidField(t, err))
}

func TestCheckCategory(t *testing.T) {
	none := domain.DefaultSettings(nil)
	require.NoError(t, none.CheckCategory(""), "no categories configured: none is asked for")
	require.Equal(t, "category", invalidField(t, none.CheckCategory("printing")))

	s := none
	s.Categories = []domain.Category{{Key: "printing", Label: "Printed records"}}
	require.NoError(t, s.CheckCategory("printing"))
	require.Equal(t, "category", invalidField(t, s.CheckCategory("")), "a category is required once any is configured")
	require.Equal(t, "category", invalidField(t, s.CheckCategory("email")))
}
