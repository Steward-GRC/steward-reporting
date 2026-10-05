// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

// MaxReasonLen bounds a decision's reason, in characters.
const MaxReasonLen = 2000

// Recipient is who received or saw the information.
type Recipient string

// The recipients.
const (
	RecipientStaffOnly           Recipient = "staff_only"
	RecipientUnknownPeople       Recipient = "unknown_people"
	RecipientAnotherOrganisation Recipient = "another_organisation"
)

// Viewed is whether the information was actually viewed.
type Viewed string

// The viewed answers.
const (
	ViewedYes      Viewed = "yes"
	ViewedProbably Viewed = "probably"
	ViewedNo       Viewed = "no"
)

// Mitigation is how far the risk was reduced.
type Mitigation string

// The mitigation answers.
const (
	MitigationFully    Mitigation = "fully"
	MitigationPartly   Mitigation = "partly"
	MitigationNotAtAll Mitigation = "not_at_all"
)

// Factors are the four answers of the guided assessment.
type Factors struct {
	Information []InformationKind
	Recipient   Recipient
	Viewed      Viewed
	Mitigation  Mitigation
}

// Suggestion is the result suggested from the factors. The officer decides.
type Suggestion string

// The suggestions.
const (
	SuggestNotify  Suggestion = "notification_likely_required"
	SuggestLowRisk Suggestion = "low_probability_of_compromise"
)

// Decision is the officer's recorded decision.
type Decision string

// The decisions.
const (
	DecisionReportable    Decision = "reportable"
	DecisionNotReportable Decision = "not_reportable"
)

// Suggest returns the suggested result. The risk is low when it was fully
// reduced, when the information was never viewed, or when only staff saw
// information that is neither health nor financial; anything else, "not
// sure" included, suggests notifying.
func Suggest(f Factors) Suggestion {
	if f.Mitigation == MitigationFully || f.Viewed == ViewedNo {
		return SuggestLowRisk
	}
	sensitive := slices.ContainsFunc(f.Information, func(k InformationKind) bool {
		return k == InfoHealth || k == InfoFinancial || k == InfoNotSure
	})
	if f.Recipient == RecipientStaffOnly && !sensitive {
		return SuggestLowRisk
	}
	return SuggestNotify
}

// CheckAssessment checks a recorded assessment: every factor answered, a
// decision and a reason.
func CheckAssessment(f Factors, d Decision, reason string) error {
	kinds, err := checkKinds(f.Information, "factors.information")
	if err != nil {
		return err
	}
	switch {
	case len(kinds) == 0:
		return errcodes.Invalid("factors.information")
	case !slices.Contains([]Recipient{RecipientStaffOnly, RecipientUnknownPeople, RecipientAnotherOrganisation}, f.Recipient):
		return errcodes.Invalid("factors.recipient")
	case !slices.Contains([]Viewed{ViewedYes, ViewedProbably, ViewedNo}, f.Viewed):
		return errcodes.Invalid("factors.viewed")
	case !slices.Contains([]Mitigation{MitigationFully, MitigationPartly, MitigationNotAtAll}, f.Mitigation):
		return errcodes.Invalid("factors.mitigation")
	case d != DecisionReportable && d != DecisionNotReportable:
		return errcodes.Invalid("decision")
	case strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > MaxReasonLen:
		return errcodes.Invalid("reason")
	}
	return nil
}
