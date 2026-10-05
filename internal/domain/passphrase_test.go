// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
)

func TestPassphraseIsStoredOnlyAsAnArgon2idHash(t *testing.T) {
	const p = "correct horse battery"
	h, err := domain.HashPassphrase(p)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$"), h)
	require.NotContains(t, h, p)

	h2, err := domain.HashPassphrase(p)
	require.NoError(t, err)
	require.NotEqual(t, h, h2, "every hash has its own salt")

	require.True(t, domain.VerifyPassphrase(p, h))
	require.False(t, domain.VerifyPassphrase("correct horse battery!", h))
	require.False(t, domain.VerifyPassphrase("", h))
}

func TestVerifyPassphraseRefusesAMalformedHash(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2id$v=19$m=19456,t=2,p=1$bad", "$bcrypt$x$y$z$w"} {
		require.False(t, domain.VerifyPassphrase("correct horse battery", h), h)
	}
}

func TestCheckPassphraseLength(t *testing.T) {
	require.Error(t, domain.CheckPassphrase("short"))
	require.Error(t, domain.CheckPassphrase("          "), "spaces alone are not a passphrase")
	require.NoError(t, domain.CheckPassphrase("ten chars!"))
	require.Error(t, domain.CheckPassphrase(strings.Repeat("a", 257)))
}

// An unknown case code still costs one hash, so the answer takes as long as
// a wrong passphrase.
func TestDummyVerifyNeverMatches(t *testing.T) {
	require.False(t, domain.DummyVerify("anything at all"))
}
