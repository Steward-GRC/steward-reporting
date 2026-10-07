// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Bugs5382/go-apperr/apperrgrpc"
	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/access"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

type users map[string]access.User

func (u users) GetUser(_ context.Context, id string) (access.User, error) {
	if id == "broken" {
		return access.User{}, errors.New("identity: connection refused")
	}
	user, ok := u[id]
	if !ok {
		return access.User{}, access.ErrNoUser
	}
	return user, nil
}

var directory = users{
	"grace":   {ID: "grace", Enabled: true, IdpGroups: []string{"Privacy Officers"}},
	"heidi":   {ID: "heidi", Enabled: true, Groups: []string{"9c1d4e2a-0000-4000-8000-000000000001"}},
	"alice":   {ID: "alice", Enabled: true},
	"carol":   {ID: "carol", Enabled: true, Roles: []string{"compliance-admin"}},
	"dave":    {ID: "dave", Enabled: true, Roles: []string{"site-admin"}},
	"root":    {ID: "root", Enabled: true, Root: true, Roles: []string{"site-admin"}},
	"ivan":    {ID: "ivan", Enabled: false, IdpGroups: []string{"privacy officers"}},
	"erin":    {ID: "erin", Enabled: true, IdpGroups: []string{"Privacy Officers Archive"}},
	"deleted": {ID: "deleted", Enabled: true, Deleted: true, IdpGroups: []string{"Privacy Officers"}},
}

func code(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	info, ok := apperrgrpc.FromError(errcodes.Error(t.Context(), err))
	require.True(t, ok)
	return info.Code
}

func TestOnlyOfficerGroupMembersAreOfficers(t *testing.T) {
	c := access.New(directory, []string{"privacy officers", "9c1d4e2a-0000-4000-8000-000000000001"})
	ctx := context.Background()
	require.NoError(t, c.Officer(ctx, "grace"), "an IdP group matches ignoring case")
	require.NoError(t, c.Officer(ctx, "heidi"), "a local group matches by id")
	for _, id := range []string{"alice", "carol", "dave", "root", "ivan", "erin", "deleted", "nobody"} {
		require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(ctx, id)), id)
	}
}

// Site admins and root read everything in the policy library; cases are
// outside it.
func TestSiteAdminAndRootAreNotOfficers(t *testing.T) {
	c := access.New(directory, []string{"Privacy Officers"})
	require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(context.Background(), "dave")))
	require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(context.Background(), "root")))
}

func TestNoOfficerGroupsMeansNoOfficers(t *testing.T) {
	c := access.New(directory, nil)
	require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(context.Background(), "grace")))
}

func TestIdentityDownRefusesTheCall(t *testing.T) {
	c := access.New(directory, []string{"Privacy Officers"})
	require.Equal(t, errcodes.CodeIdentityUnavailable, code(t, c.Officer(context.Background(), "broken")))
}

func TestOfficerGroupsAreReadOnEveryCall(t *testing.T) {
	groups := []string{"Privacy Officers"}
	c := access.NewFromSource(directory, func(context.Context) ([]string, error) { return groups, nil })
	ctx := context.Background()
	require.NoError(t, c.Officer(ctx, "grace"))
	groups = []string{"9c1d4e2a-0000-4000-8000-000000000001"}
	require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(ctx, "grace")), "a removed group stops admitting at once")
	require.NoError(t, c.Officer(ctx, "heidi"))
}

func TestOfficerGroupsUnreadableRefusesTheCall(t *testing.T) {
	c := access.NewFromSource(directory, func(context.Context) ([]string, error) {
		return nil, errcodes.StoreUnavailable("settings", errors.New("connection refused"))
	})
	require.Equal(t, errcodes.CodeStoreUnavailable, code(t, c.Officer(context.Background(), "grace")))
}

// The Compliance settings are changed by holders of compliance.manage:
// compliance admins, and site admins and root through the catalog.
func TestSettingsAdminHoldsComplianceManage(t *testing.T) {
	c := access.New(directory, []string{"Privacy Officers"})
	ctx := context.Background()
	for _, id := range []string{"carol", "dave", "root"} {
		require.NoError(t, c.SettingsAdmin(ctx, id), id)
	}
	for _, id := range []string{"grace", "alice", "nobody"} {
		require.Equal(t, errcodes.CodeSettingsAccessDenied, code(t, c.SettingsAdmin(ctx, id)), id)
	}
	disabled := users{"carol": {ID: "carol", Enabled: false, Roles: []string{"compliance-admin"}}}
	require.Equal(t, errcodes.CodeSettingsAccessDenied, code(t, access.New(disabled, nil).SettingsAdmin(ctx, "carol")))
	require.Equal(t, errcodes.CodeIdentityUnavailable, code(t, c.SettingsAdmin(ctx, "broken")))
}
