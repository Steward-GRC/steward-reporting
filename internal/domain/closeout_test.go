// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
)

func TestCheckCloseOut(t *testing.T) {
	acts := []domain.CorrectiveAction{
		{Description: "Badge release on print room printers", PolicyID: "5b0b7f0e-7a51-4d38-9a0c-2f4b1d7f6a10"},
		{Description: "Remind staff about printouts"},
	}
	require.NoError(t, domain.CheckCloseOut(domain.OutcomeSubstantiated, acts, "Thank you for speaking up."))
	require.Equal(t, "outcome", field(t, domain.CheckCloseOut("", acts, "")))
	bad := []domain.CorrectiveAction{{Description: "x", PolicyID: "VSP-001"}}
	require.Equal(t, "corrective_actions.policy_id", field(t, domain.CheckCloseOut(domain.OutcomeInconclusive, bad, "")))
	empty := []domain.CorrectiveAction{{Description: " "}}
	require.Equal(t, "corrective_actions.description", field(t, domain.CheckCloseOut(domain.OutcomeInconclusive, empty, "")))
	require.Equal(t, "closing_message", field(t, domain.CheckCloseOut(domain.OutcomeInconclusive, nil, strings.Repeat("a", 10001))))
}
