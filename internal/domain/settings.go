// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

// Settings bounds.
const (
	// DefaultRetentionDays keeps a closed case for seven years.
	DefaultRetentionDays = 7 * 365
	// MinRetentionDays keeps a mistyped setting from purging recent cases.
	MinRetentionDays  = 30
	MaxRetentionDays  = 100 * 365
	MaxCategories     = 50
	MaxCategoryKeyLen = 40
)

var categoryKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Category is one intake category a reporter picks from. The key is stored
// on the case; the label is what the form shows.
type Category struct {
	Key   string
	Label string
}

// Settings are the Compliance settings (C10).
type Settings struct {
	// PublicLink lets anonymous reports in. Off, new anonymous reports are
	// refused; existing ones can still be checked and replied to.
	PublicLink bool
	// RetentionDays is how long a closed case is kept after it closed.
	RetentionDays int
	// Categories are the intake categories, in form order. Empty means the
	// form asks for none.
	Categories []Category
	UpdatedAt  time.Time
	// UpdatedBy is the user who saved them last; empty for the seed.
	UpdatedBy string
}

// DefaultSettings are the settings before anyone saves them.
func DefaultSettings() Settings {
	return Settings{PublicLink: true, RetentionDays: DefaultRetentionDays}
}

// Normalize trims and checks the settings.
func (s Settings) Normalize() (Settings, error) {
	if s.RetentionDays < MinRetentionDays || s.RetentionDays > MaxRetentionDays {
		return Settings{}, errcodes.Invalid("retention_days")
	}
	if len(s.Categories) > MaxCategories {
		return Settings{}, errcodes.Invalid("intake_categories")
	}
	cats := make([]Category, 0, len(s.Categories))
	for _, c := range s.Categories {
		c.Label = strings.TrimSpace(c.Label)
		if len(c.Key) > MaxCategoryKeyLen || !categoryKey.MatchString(c.Key) ||
			c.Label == "" || utf8.RuneCountInString(c.Label) > MaxShortFieldLen ||
			slices.ContainsFunc(cats, func(o Category) bool { return o.Key == c.Key }) {
			return Settings{}, errcodes.Invalid("intake_categories")
		}
		cats = append(cats, c)
	}
	s.Categories = cats
	return s, nil
}

// CheckCategory checks the category a report was filed under: one of the
// configured keys, or none while no category is configured.
func (s Settings) CheckCategory(key string) error {
	if len(s.Categories) == 0 {
		if key != "" {
			return errcodes.Invalid("category")
		}
		return nil
	}
	if !slices.ContainsFunc(s.Categories, func(c Category) bool { return c.Key == key }) {
		return errcodes.Invalid("category")
	}
	return nil
}
