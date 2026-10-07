// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc_test

import (
	"testing"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

func categories() []*reportingv1.IntakeCategory {
	return []*reportingv1.IntakeCategory{
		{Key: "printing", Label: "Printed records left out"},
		{Key: "email", Label: "Email sent to the wrong person"},
	}
}

func (e *env) save(t *testing.T, s *reportingv1.ComplianceSettings) *reportingv1.ComplianceSettings {
	t.Helper()
	r, err := e.sets.UpdateSettings(as(t, carol), &reportingv1.UpdateSettingsRequest{Settings: s})
	require.NoError(t, err)
	return r.GetSettings()
}

func TestSettingsStartFromTheSeed(t *testing.T) {
	e := newEnv(t)
	r, err := e.sets.GetSettings(as(t, carol), &reportingv1.GetSettingsRequest{})
	require.NoError(t, err)
	s := r.GetSettings()
	require.Equal(t, []string{officerGroup}, s.GetOfficerGroups(), "REPORTING_OFFICER_GROUPS seeds the officer groups")
	require.True(t, s.GetPublicLinkEnabled())
	require.EqualValues(t, domain.DefaultRetentionDays, s.GetRetentionDays())
	require.Empty(t, s.GetIntakeCategories())
	require.Empty(t, s.GetUpdatedByUserId())

	again, err := e.store.SeedSettings(t.Context(), domain.DefaultSettings([]string{"Someone Else"}))
	require.NoError(t, err)
	require.False(t, again, "the seed applies to a fresh database only")
}

func TestSettingsRoundTripAndAudit(t *testing.T) {
	e := newEnv(t)
	saved := e.save(t, &reportingv1.ComplianceSettings{
		OfficerGroups: []string{officerGroup, " Privacy Leads "}, PublicLinkEnabled: false, RetentionDays: 3650,
		IntakeCategories: categories(),
	})
	require.Equal(t, []string{officerGroup, "Privacy Leads"}, saved.GetOfficerGroups())
	require.False(t, saved.GetPublicLinkEnabled())
	require.EqualValues(t, 3650, saved.GetRetentionDays())
	require.Equal(t, carol, saved.GetUpdatedByUserId())
	require.NotNil(t, saved.GetUpdatedAt())

	got, err := e.sets.GetSettings(as(t, dave), &reportingv1.GetSettingsRequest{})
	require.NoError(t, err)
	require.True(t, proto.Equal(saved, got.GetSettings()))

	evs := e.events(t)
	require.Equal(t, []string{"settings.updated", "settings.read"}, actions(evs))
	require.Equal(t, carol, evs[0].ActorUserID)
	require.Equal(t, map[string]string{"officer_groups": "2", "public_link": "false", "retention_days": "3650", "intake_categories": "2"}, evs[0].Attributes)
	require.Equal(t, dave, evs[1].ActorUserID)
}

func TestOnlySettingsAdminsReachTheSettings(t *testing.T) {
	e := newEnv(t)
	for _, who := range []string{carol, dave, root} {
		_, err := e.sets.GetSettings(as(t, who), &reportingv1.GetSettingsRequest{})
		require.NoError(t, err, who)
	}
	for _, who := range []string{grace, alice, bob} {
		_, err := e.sets.GetSettings(as(t, who), &reportingv1.GetSettingsRequest{})
		require.Equal(t, errcodes.CodeSettingsAccessDenied, errCode(t, err), who)
		_, err = e.sets.UpdateSettings(as(t, who), &reportingv1.UpdateSettingsRequest{Settings: &reportingv1.ComplianceSettings{RetentionDays: 365}})
		require.Equal(t, errcodes.CodeSettingsAccessDenied, errCode(t, err), who)
	}
	_, err := e.sets.GetSettings(t.Context(), &reportingv1.GetSettingsRequest{})
	require.Equal(t, errcodes.CodeSignInRequired, errCode(t, err))
	actingAs := grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: carol, Impersonator: dave})
	_, err = e.sets.UpdateSettings(actingAs, &reportingv1.UpdateSettingsRequest{Settings: &reportingv1.ComplianceSettings{RetentionDays: 365}})
	require.Equal(t, errcodes.CodeActAsNotAllowed, errCode(t, err))

	refused := 0
	for _, ev := range e.events(t) {
		if ev.Action == "settings.access_refused" {
			refused++
		}
	}
	require.Equal(t, 7, refused, "each refusal of a known user is audited")
}

func TestUpdateSettingsValidates(t *testing.T) {
	e := newEnv(t)
	_, err := e.sets.UpdateSettings(as(t, carol), &reportingv1.UpdateSettingsRequest{Settings: &reportingv1.ComplianceSettings{RetentionDays: 1}})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err))
	_, err = e.sets.UpdateSettings(as(t, carol), &reportingv1.UpdateSettingsRequest{})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err))
}

// Officer access follows the stored groups from the next call on.
func TestOfficerAccessReadsTheStoredGroups(t *testing.T) {
	e := newEnv(t)
	_, err := e.cases.ListCases(as(t, grace), &reportingv1.ListCasesRequest{})
	require.NoError(t, err)
	e.save(t, &reportingv1.ComplianceSettings{OfficerGroups: []string{"Privacy Leads"}, PublicLinkEnabled: true, RetentionDays: 365})
	_, err = e.cases.ListCases(as(t, grace), &reportingv1.ListCasesRequest{})
	require.Equal(t, errcodes.CodeCaseAccessDenied, errCode(t, err))
}

func TestPublicLinkOffRefusesNewAnonymousReportsOnly(t *testing.T) {
	e := newEnv(t)
	code, _ := e.submitAnonymous(t)
	e.save(t, &reportingv1.ComplianceSettings{OfficerGroups: []string{officerGroup}, PublicLinkEnabled: false, RetentionDays: 365})

	opts, err := e.intake.GetIntakeOptions(t.Context(), &reportingv1.GetIntakeOptionsRequest{})
	require.NoError(t, err)
	require.False(t, opts.GetAnonymousReportsOpen())

	_, err = e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: details(), Passphrase: passphrase})
	require.Equal(t, errcodes.CodePublicLinkOff, errCode(t, err))

	_, err = e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.NoError(t, err, "an existing report can still be checked")
	_, err = e.intake.ReplyToReport(t.Context(), &reportingv1.ReplyToReportRequest{CaseCode: code, Passphrase: passphrase, Body: "One more thing."})
	require.NoError(t, err, "and replied to")
	_, err = e.intake.SubmitNamedReport(as(t, alice), &reportingv1.SubmitNamedReportRequest{Details: details()})
	require.NoError(t, err, "named reports don't use the public link")

	list, err := e.cases.ListCases(as(t, grace), &reportingv1.ListCasesRequest{})
	require.NoError(t, err)
	require.Len(t, list.GetCases(), 2, "the refused report stored nothing")
}

func TestIntakeCategoriesAreCheckedOnSubmit(t *testing.T) {
	e := newEnv(t)
	withCategory := func(key string) *reportingv1.ReportDetails {
		d := details()
		d.Category = key
		return d
	}
	_, err := e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: withCategory("printing"), Passphrase: passphrase})
	require.Equal(t, errcodes.CodeInvalid, errCode(t, err), "no categories configured: none may be sent")

	e.save(t, &reportingv1.ComplianceSettings{OfficerGroups: []string{officerGroup}, PublicLinkEnabled: true, RetentionDays: 365, IntakeCategories: categories()})
	opts, err := e.intake.GetIntakeOptions(t.Context(), &reportingv1.GetIntakeOptionsRequest{})
	require.NoError(t, err)
	require.True(t, opts.GetAnonymousReportsOpen())
	require.Len(t, opts.GetCategories(), 2)

	for _, bad := range []string{"", "parking"} {
		_, err = e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: withCategory(bad), Passphrase: passphrase})
		require.Equal(t, errcodes.CodeInvalid, errCode(t, err), "anonymous %q", bad)
		_, err = e.intake.SubmitNamedReport(as(t, alice), &reportingv1.SubmitNamedReportRequest{Details: withCategory(bad)})
		require.Equal(t, errcodes.CodeInvalid, errCode(t, err), "named %q", bad)
	}
	r, err := e.intake.SubmitNamedReport(as(t, alice), &reportingv1.SubmitNamedReportRequest{Details: withCategory("email")})
	require.NoError(t, err)
	c, err := e.cases.GetCase(as(t, grace), &reportingv1.GetCaseRequest{CaseId: r.GetCaseId()})
	require.NoError(t, err)
	require.Equal(t, "email", c.GetCase().GetDetails().GetCategory())
}
