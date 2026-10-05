// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"strings"
	"testing"

	"github.com/Bugs5382/go-apperr/apperrgrpc"
	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

func field(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	info, ok := apperrgrpc.FromError(errcodes.Error(t.Context(), err))
	require.True(t, ok)
	require.Equal(t, errcodes.CodeInvalid, info.Code)
	return info.Metadata["field"]
}

func TestDetailsValidate(t *testing.T) {
	ok := domain.Details{WhatHappened: "A printed list was left on the shared printer.", Occurred: "About 2 Oct",
		Location: "Second floor, print room", InformationKinds: []domain.InformationKind{domain.InfoHealth, domain.InfoHealth},
		StillHappening: domain.AnswerNo}
	got, err := ok.Normalize()
	require.NoError(t, err)
	require.Equal(t, []domain.InformationKind{domain.InfoHealth}, got.InformationKinds, "kinds are deduplicated")

	blank := ok
	blank.WhatHappened = "   "
	require.Equal(t, "what_happened", field(t, func() error { _, err := blank.Normalize(); return err }()))

	long := ok
	long.Location = strings.Repeat("x", 201)
	require.Equal(t, "location", field(t, func() error { _, err := long.Normalize(); return err }()))

	bad := ok
	bad.InformationKinds = []domain.InformationKind{"gossip"}
	require.Equal(t, "information_kinds", field(t, func() error { _, err := bad.Normalize(); return err }()))
}

func TestSummaryIsTheFirstLineCut(t *testing.T) {
	require.Equal(t, "Patient list left on a shared printer", domain.Summary("Patient list left on a shared printer\nMore detail."))
	s := domain.Summary(strings.Repeat("é", 300))
	require.Equal(t, 120, len([]rune(s)))
}

func TestCheckBody(t *testing.T) {
	require.NoError(t, domain.CheckBody("About one page, maybe 30 names."))
	require.Equal(t, "body", field(t, domain.CheckBody(" ")))
	require.Equal(t, "body", field(t, domain.CheckBody(strings.Repeat("a", 10001))))
}

func TestStatusChangesNeverClose(t *testing.T) {
	for _, s := range []domain.Status{domain.StatusNew, domain.StatusInReview, domain.StatusNeedsReporterReply,
		domain.StatusRiskAssessment, domain.StatusNotificationDue} {
		require.NoError(t, domain.CheckSettableStatus(s))
	}
	require.Equal(t, "status", field(t, domain.CheckSettableStatus(domain.StatusClosed)))
	require.Equal(t, "status", field(t, domain.CheckSettableStatus("")))
}
