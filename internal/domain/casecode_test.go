// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
)

var codeShape = regexp.MustCompile(`^[23456789ABCDEFGHJKMNPQRSTVWXYZ]{3}-[23456789ABCDEFGHJKMNPQRSTVWXYZ]{3}-[23456789ABCDEFGHJKMNPQRSTVWXYZ]{2}$`)

func TestNewCaseCodeIsGroupedInThreesFromAnUnambiguousAlphabet(t *testing.T) {
	seen := map[string]bool{}
	for range 500 {
		c, err := domain.NewCaseCode()
		require.NoError(t, err)
		require.Regexp(t, codeShape, c)
		require.False(t, seen[c], "codes repeat: %s", c)
		seen[c] = true
	}
}

func TestNormalizeCaseCodeAcceptsWhatAReporterTypes(t *testing.T) {
	for _, in := range []string{"7KQ-42M-RX", "7kq42mrx", " 7KQ 42M RX ", "7kq-42m-rx"} {
		got, ok := domain.NormalizeCaseCode(in)
		require.True(t, ok, in)
		require.Equal(t, "7KQ-42M-RX", got)
	}
	for _, in := range []string{"", "7KQ-42M-R", "7KQ-42M-RXX", "7KQ-42M-R0", "OIL-42M-RX"} {
		_, ok := domain.NormalizeCaseCode(in)
		require.False(t, ok, in)
	}
}
