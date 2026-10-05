// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

// Close-out bounds.
const (
	MaxCorrectiveActions = 50
	maxActionLen         = 1000
)

// Outcome is how a case closed.
type Outcome string

// The outcomes.
const (
	OutcomeSubstantiated    Outcome = "substantiated"
	OutcomeNotSubstantiated Outcome = "not_substantiated"
	OutcomeInconclusive     Outcome = "inconclusive"
)

// CorrectiveAction is one action taken, optionally changing a core policy.
type CorrectiveAction struct {
	Description string
	// PolicyID is core's policy id, a UUID; empty for none.
	PolicyID string
}

// CheckCloseOut checks a close-out.
func CheckCloseOut(o Outcome, actions []CorrectiveAction, closingMessage string) error {
	switch o {
	case OutcomeSubstantiated, OutcomeNotSubstantiated, OutcomeInconclusive:
	default:
		return errcodes.Invalid("outcome")
	}
	if len(actions) > MaxCorrectiveActions {
		return errcodes.Invalid("corrective_actions")
	}
	for _, a := range actions {
		if strings.TrimSpace(a.Description) == "" || utf8.RuneCountInString(a.Description) > maxActionLen {
			return errcodes.Invalid("corrective_actions.description")
		}
		if a.PolicyID != "" {
			if _, err := uuid.Parse(a.PolicyID); err != nil {
				return errcodes.Invalid("corrective_actions.policy_id")
			}
		}
	}
	if utf8.RuneCountInString(closingMessage) > MaxBodyLen {
		return errcodes.Invalid("closing_message")
	}
	return nil
}
