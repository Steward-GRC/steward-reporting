// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// caseCodeAlphabet leaves out 0, 1, I, L, O and U, so a code read aloud or
// copied by hand can't be mistaken.
const caseCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

const caseCodeLen = 8

// NewCaseCode returns a random case code, grouped in threes ("7KQ-42M-RX").
func NewCaseCode() (string, error) {
	var b [caseCodeLen]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("domain: case code: %w", err)
	}
	raw := make([]byte, caseCodeLen)
	for i, v := range b {
		// 256 is not a multiple of 30; the slight bias is irrelevant next to
		// the passphrase, which is what keeps a report closed.
		raw[i] = caseCodeAlphabet[int(v)%len(caseCodeAlphabet)]
	}
	return group(string(raw)), nil
}

// NormalizeCaseCode turns what a reporter typed into the stored form. It
// ignores case, spaces and dashes; ok is false for anything that can't be a
// case code.
func NormalizeCaseCode(in string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(in) {
		switch {
		case r == ' ' || r == '-':
		case strings.ContainsRune(caseCodeAlphabet, r):
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	if b.Len() != caseCodeLen {
		return "", false
	}
	return group(b.String()), true
}

func group(raw string) string { return raw[0:3] + "-" + raw[3:6] + "-" + raw[6:8] }
