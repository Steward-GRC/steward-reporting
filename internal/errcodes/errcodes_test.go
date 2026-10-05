// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package errcodes_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Bugs5382/go-apperr/apperrgrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

func TestStoreUnavailableHidesItsCause(t *testing.T) {
	st := status.Convert(errcodes.Error(context.Background(),
		errcodes.StoreUnavailable("insert_case", errors.New("dial tcp 192.0.2.10:5432: connect: connection refused"))))
	require.Equal(t, codes.Internal, st.Code())
	require.Equal(t, "Code 8101: Internal Error", st.Message(), "a store cause never reaches the wire")

	info, ok := apperrgrpc.FromStatus(st)
	require.True(t, ok, "status must carry ErrorInfo")
	require.Equal(t, "REPORT_STORE_UNAVAILABLE", info.Symbol)
	require.Equal(t, 8101, info.Code)
	require.Equal(t, "reporting", info.Domain)
	require.Equal(t, "insert_case", info.Metadata["op"])
}

func TestUserSafeCodesKeepTheirMessage(t *testing.T) {
	cases := []struct {
		err  error
		code codes.Code
		num  int
		meta map[string]string
	}{
		{errcodes.Invalid("what_happened"), codes.InvalidArgument, 8102, map[string]string{"field": "what_happened"}},
		{errcodes.ReportNotFound(), codes.NotFound, 8103, nil},
		{errcodes.SignInRequired(), codes.Unauthenticated, 8104, nil},
		{errcodes.CaseAccessDenied(), codes.PermissionDenied, 8105, nil},
		{errcodes.CaseNotFound(), codes.NotFound, 8106, nil},
		{errcodes.CaseClosed(), codes.FailedPrecondition, 8107, nil},
		{errcodes.AttachmentUnsupported(2), codes.InvalidArgument, 8108, map[string]string{"attachment": "2"}},
		{errcodes.ActAsNotAllowed(), codes.PermissionDenied, 8110, nil},
	}
	for _, c := range cases {
		st := status.Convert(errcodes.Error(context.Background(), c.err))
		require.Equal(t, c.code, st.Code(), "code %d", c.num)
		entry, ok := errcodes.Registry().Describe(c.num)
		require.True(t, ok)
		require.True(t, entry.UserSafe)
		require.Equal(t, entry.Message, st.Message())
		info, _ := apperrgrpc.FromStatus(st)
		require.Equal(t, c.num, info.Code)
		for k, v := range c.meta {
			require.Equal(t, v, info.Metadata[k])
		}
	}
}

// One answer for an unknown case code and a wrong passphrase: the error must
// not say which of the two was wrong.
func TestReportNotFoundSaysNothingAboutWhy(t *testing.T) {
	st := status.Convert(errcodes.Error(context.Background(), errcodes.ReportNotFound()))
	info, _ := apperrgrpc.FromStatus(st)
	require.Empty(t, info.Metadata)
	require.NotContains(t, st.Message(), "passphrase is wrong")
}

func TestIdentityUnavailableIsNotUserSafe(t *testing.T) {
	st := status.Convert(errcodes.Error(context.Background(), errcodes.IdentityUnavailable(errors.New("identity: connection refused"))))
	require.Equal(t, codes.Unavailable, st.Code())
	require.Equal(t, "Code 8109: Internal Error", st.Message())
}

func TestUncodedErrorsFallBackToInternal(t *testing.T) {
	info, _ := apperrgrpc.FromError(errcodes.Error(context.Background(), errors.New("boom")))
	require.Equal(t, errcodes.CodeInternal, info.Code)
	require.Equal(t, "INTERNAL", info.Symbol)
}

func TestRegistryBand(t *testing.T) {
	require.NotEmpty(t, errcodes.Entries())
	for _, e := range errcodes.Entries() {
		require.Equal(t, 81, e.Code/100, "code %d must be in 8100 to 8199", e.Code)
		_, ok := errcodes.Registry().Describe(e.Code)
		require.True(t, ok)
	}
}

// docs/error-codes.md is generated from the registry; refresh it with
// UPDATE_DOCS=1 go test ./internal/errcodes.
func TestErrorCodesDocIsCurrent(t *testing.T) {
	const path = "../../docs/error-codes.md"
	want := errcodes.Doc()
	if os.Getenv("UPDATE_DOCS") == "1" {
		require.NoError(t, os.WriteFile(path, []byte(want), 0o600))
	}
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want, string(got), "docs/error-codes.md is stale; run UPDATE_DOCS=1 go test ./internal/errcodes")
}
