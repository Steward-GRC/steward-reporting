// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc

import (
	"context"
	"errors"
	"strconv"

	log "github.com/Bugs5382/go-log"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/access"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

// OfficerGroups reads the officer groups from the stored settings, for
// access.NewFromSource.
func OfficerGroups(st *store.Store) access.GroupSource {
	return func(ctx context.Context) ([]string, error) {
		s, err := loadSettings(ctx, st)
		if err != nil {
			return nil, err
		}
		return s.OfficerGroups, nil
	}
}

// loadSettings returns the stored settings, or the defaults with no officer
// groups before the start-up seed has run.
func loadSettings(ctx context.Context, st *store.Store) (domain.Settings, error) {
	s, err := st.Settings(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return domain.DefaultSettings(nil), nil
	}
	if err != nil {
		return domain.Settings{}, errcodes.StoreUnavailable("load settings", err)
	}
	return s, nil
}

// Settings serves SettingsService.
type Settings struct {
	reportingv1.UnimplementedSettingsServiceServer
	d Deps
}

// NewSettings returns the Compliance settings service.
func NewSettings(d Deps) *Settings { return &Settings{d: d} }

// admin returns the calling settings admin, or the coded refusal. A refusal
// of a known actor is audited.
func (s *Settings) admin(ctx context.Context) (string, error) {
	who, err := signedIn(ctx)
	if isCode(err, errcodes.CodeActAsNotAllowed) {
		return "", s.refused(ctx, realUser(ctx), err)
	}
	if err != nil {
		return "", err
	}
	if err := s.d.Officers.SettingsAdmin(ctx, who); err != nil {
		if isCode(err, errcodes.CodeSettingsAccessDenied) {
			return "", s.refused(ctx, who, err)
		}
		return "", err
	}
	return who, nil
}

func (s *Settings) refused(ctx context.Context, who string, cause error) error {
	s.d.Log.Ctx(ctx).Warn("settings call refused", log.F("user_id", who))
	if err := s.d.inTx(ctx, func(ctx context.Context) error {
		return s.d.emit(ctx, "settings.access_refused", who, "", nil)
	}); err != nil {
		return err
	}
	return cause
}

// GetSettings returns the settings as they stand.
func (s *Settings) GetSettings(ctx context.Context, _ *reportingv1.GetSettingsRequest) (*reportingv1.GetSettingsResponse, error) {
	who, err := s.admin(ctx)
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var st domain.Settings
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if st, err = loadSettings(ctx, s.d.Store); err != nil {
			return err
		}
		return s.d.emit(ctx, "settings.read", who, "", nil)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.GetSettingsResponse{Settings: settingsToProto(st)}, nil
}

// UpdateSettings replaces every setting at once.
func (s *Settings) UpdateSettings(ctx context.Context, req *reportingv1.UpdateSettingsRequest) (*reportingv1.UpdateSettingsResponse, error) {
	who, err := s.admin(ctx)
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	if req.GetSettings() == nil {
		return nil, errcodes.Error(ctx, errcodes.Invalid("settings"))
	}
	st, err := settingsFromProto(req.GetSettings()).Normalize()
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var saved domain.Settings
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		if err := s.d.Store.SaveSettings(ctx, st, who); err != nil {
			return errcodes.StoreUnavailable("save settings", err)
		}
		var err error
		if saved, err = loadSettings(ctx, s.d.Store); err != nil {
			return err
		}
		return s.d.emit(ctx, "settings.updated", who, "", map[string]string{
			"officer_groups":    strconv.Itoa(len(saved.OfficerGroups)),
			"public_link":       strconv.FormatBool(saved.PublicLink),
			"retention_days":    strconv.Itoa(saved.RetentionDays),
			"intake_categories": strconv.Itoa(len(saved.Categories)),
		})
	})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	s.d.Log.Ctx(ctx).Info("compliance settings saved", log.F("user_id", who), log.F("officer_groups", len(saved.OfficerGroups)),
		log.F("public_link", saved.PublicLink), log.F("retention_days", saved.RetentionDays), log.F("intake_categories", len(saved.Categories)))
	return &reportingv1.UpdateSettingsResponse{Settings: settingsToProto(saved)}, nil
}

func settingsFromProto(p *reportingv1.ComplianceSettings) domain.Settings {
	s := domain.Settings{OfficerGroups: p.GetOfficerGroups(), PublicLink: p.GetPublicLinkEnabled(), RetentionDays: int(p.GetRetentionDays())}
	for _, c := range p.GetIntakeCategories() {
		s.Categories = append(s.Categories, domain.Category{Key: c.GetKey(), Label: c.GetLabel()})
	}
	return s
}

func categoriesToProto(in []domain.Category) []*reportingv1.IntakeCategory {
	out := make([]*reportingv1.IntakeCategory, len(in))
	for i, c := range in {
		out[i] = &reportingv1.IntakeCategory{Key: c.Key, Label: c.Label}
	}
	return out
}

func settingsToProto(s domain.Settings) *reportingv1.ComplianceSettings {
	p := &reportingv1.ComplianceSettings{
		OfficerGroups: s.OfficerGroups, PublicLinkEnabled: s.PublicLink, RetentionDays: int32(s.RetentionDays), // #nosec G115 -- 30 to 36500, checked by Normalize and the schema
		IntakeCategories: categoriesToProto(s.Categories), UpdatedByUserId: s.UpdatedBy,
	}
	if !s.UpdatedAt.IsZero() {
		p.UpdatedAt = ts(s.UpdatedAt)
	}
	return p
}
