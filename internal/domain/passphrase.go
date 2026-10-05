// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

// Passphrase length bounds, in characters.
const (
	MinPassphraseLen = 10
	MaxPassphraseLen = 256
)

// argon2id parameters: the OWASP minimum (19 MiB, two passes, one lane).
const (
	argonMemoryKiB = 19456
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

var b64 = base64.RawStdEncoding

// CheckPassphrase checks a passphrase the reporter chose.
func CheckPassphrase(p string) error {
	n := utf8.RuneCountInString(p)
	if strings.TrimSpace(p) == "" || n < MinPassphraseLen || n > MaxPassphraseLen {
		return errcodes.Invalid("passphrase")
	}
	return nil
}

// HashPassphrase returns the argon2id hash of p in the PHC string format.
// The passphrase itself is never stored.
func HashPassphrase(p string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("domain: passphrase salt: %w", err)
	}
	key := argon2.IDKey([]byte(p), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassphrase reports whether p matches the stored hash. A malformed
// hash never matches.
func VerifyPassphrase(p, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil || threads == 0 {
		return false
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(p), salt, passes, memory, threads, uint32(len(want))) // #nosec G115 -- a stored 32-byte key
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked when a case code matches nothing, so an unknown code
// costs as much as a wrong passphrase.
var dummyHash = func() string {
	h, err := HashPassphrase("no report has this passphrase")
	if err != nil {
		panic(err)
	}
	return h
}()

// DummyVerify does the work of one verification and always fails.
func DummyVerify(p string) bool {
	_ = VerifyPassphrase(p, dummyHash)
	return false
}
