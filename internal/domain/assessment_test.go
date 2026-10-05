// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
)

func TestSuggestFollowsTheFourFactors(t *testing.T) {
	health := []domain.InformationKind{domain.InfoHealth}
	contact := []domain.InformationKind{domain.InfoContact}
	cases := []struct {
		name string
		f    domain.Factors
		want domain.Suggestion
	}{
		{"the design's example: health information, unknown people, probably viewed, partly reduced",
			domain.Factors{health, domain.RecipientUnknownPeople, domain.ViewedProbably, domain.MitigationPartly}, domain.SuggestNotify},
		{"risk fully reduced", domain.Factors{health, domain.RecipientUnknownPeople, domain.ViewedYes, domain.MitigationFully}, domain.SuggestLowRisk},
		{"never viewed", domain.Factors{health, domain.RecipientAnotherOrganisation, domain.ViewedNo, domain.MitigationNotAtAll}, domain.SuggestLowRisk},
		{"contact details seen by staff only", domain.Factors{contact, domain.RecipientStaffOnly, domain.ViewedYes, domain.MitigationPartly}, domain.SuggestLowRisk},
		{"health information seen by staff", domain.Factors{health, domain.RecipientStaffOnly, domain.ViewedYes, domain.MitigationPartly}, domain.SuggestNotify},
		{"not sure what was involved", domain.Factors{[]domain.InformationKind{domain.InfoNotSure}, domain.RecipientStaffOnly, domain.ViewedYes, domain.MitigationNotAtAll}, domain.SuggestNotify},
	}
	for _, c := range cases {
		require.Equal(t, c.want, domain.Suggest(c.f), c.name)
	}
}

func TestAssessmentValidate(t *testing.T) {
	f := domain.Factors{[]domain.InformationKind{domain.InfoHealth}, domain.RecipientUnknownPeople, domain.ViewedProbably, domain.MitigationPartly}
	require.NoError(t, domain.CheckAssessment(f, domain.DecisionReportable, "Unattended for about 2 hours."))
	require.Equal(t, "reason", field(t, domain.CheckAssessment(f, domain.DecisionReportable, " ")))
	require.Equal(t, "decision", field(t, domain.CheckAssessment(f, "", "why")))
	missing := f
	missing.Viewed = ""
	require.Equal(t, "factors.viewed", field(t, domain.CheckAssessment(missing, domain.DecisionNotReportable, "why")))
	none := f
	none.Information = nil
	require.Equal(t, "factors.information", field(t, domain.CheckAssessment(none, domain.DecisionNotReportable, "why")))
}
