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
	"grace":   {ID: "grace", Enabled: true, Groups: []string{"officers"}, Roles: []string{"privacy-officer"}},
	"heidi":   {ID: "heidi", Enabled: true, Roles: []string{"compliance-admin", "privacy-officer"}},
	"alice":   {ID: "alice", Enabled: true},
	"carol":   {ID: "carol", Enabled: true, Roles: []string{"compliance-admin"}},
	"dave":    {ID: "dave", Enabled: true, Roles: []string{"site-admin"}},
	"root":    {ID: "root", Enabled: true, Root: true, Roles: []string{"site-admin"}},
	"ivan":    {ID: "ivan", Enabled: false, Roles: []string{"privacy-officer"}},
	"erin":    {ID: "erin", Enabled: true, Roles: []string{"privacy-officers", "Privacy-Officer"}},
	"deleted": {ID: "deleted", Enabled: true, Deleted: true, Roles: []string{"privacy-officer"}},
}

func code(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	info, ok := apperrgrpc.FromError(errcodes.Error(t.Context(), err))
	require.True(t, ok)
	return info.Code
}

func TestOnlyHoldersOfThePrivacyOfficerRoleAreOfficers(t *testing.T) {
	c := access.New(directory)
	ctx := context.Background()
	require.NoError(t, c.Officer(ctx, "grace"))
	require.NoError(t, c.Officer(ctx, "heidi"), "the role may sit beside others")
	for _, id := range []string{"alice", "carol", "dave", "root", "ivan", "erin", "deleted", "nobody"} {
		require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(ctx, id)), id)
	}
}

// Site admins and root read everything in the policy library; cases are
// outside it.
func TestSiteAdminAndRootAreNotOfficers(t *testing.T) {
	c := access.New(directory)
	require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(context.Background(), "dave")))
	require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(context.Background(), "root")))
}

// A group of any name, local or from the identity provider, admits nobody.
func TestGroupMembershipIsNotTheOfficerRole(t *testing.T) {
	c := access.New(users{"gina": {ID: "gina", Enabled: true, Roles: []string{"author"}}})
	require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(context.Background(), "gina")))
}

func TestRoleIsReadOnEveryCall(t *testing.T) {
	d := users{"grace": {ID: "grace", Enabled: true, Roles: []string{"privacy-officer"}}}
	c := access.New(d)
	ctx := context.Background()
	require.NoError(t, c.Officer(ctx, "grace"))
	d["grace"] = access.User{ID: "grace", Enabled: true}
	require.Equal(t, errcodes.CodeCaseAccessDenied, code(t, c.Officer(ctx, "grace")), "a revoked role stops admitting at once")
}

func TestIdentityDownRefusesTheCall(t *testing.T) {
	c := access.New(directory)
	require.Equal(t, errcodes.CodeIdentityUnavailable, code(t, c.Officer(context.Background(), "broken")))
}

// The Compliance settings are changed by holders of compliance.manage:
// compliance admins, and site admins and root through the catalog.
func TestSettingsAdminHoldsComplianceManage(t *testing.T) {
	c := access.New(directory)
	ctx := context.Background()
	for _, id := range []string{"carol", "dave", "root"} {
		require.NoError(t, c.SettingsAdmin(ctx, id), id)
	}
	for _, id := range []string{"grace", "alice", "nobody"} {
		require.Equal(t, errcodes.CodeSettingsAccessDenied, code(t, c.SettingsAdmin(ctx, id)), id)
	}
	disabled := users{"carol": {ID: "carol", Enabled: false, Roles: []string{"compliance-admin"}}}
	require.Equal(t, errcodes.CodeSettingsAccessDenied, code(t, access.New(disabled).SettingsAdmin(ctx, "carol")))
	require.Equal(t, errcodes.CodeIdentityUnavailable, code(t, c.SettingsAdmin(ctx, "broken")))
}
