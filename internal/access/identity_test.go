// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/Steward-GRC/steward-reporting/gen/go/thirdparty/identity/v1"
	"github.com/Steward-GRC/steward-reporting/internal/access"
)

type fakeIdentity struct {
	user  *identityv1.User
	err   error
	asked []string
}

func (f *fakeIdentity) GetUser(_ context.Context, in *identityv1.GetUserRequest, _ ...grpc.CallOption) (*identityv1.GetUserResponse, error) {
	f.asked = append(f.asked, in.GetUserId())
	return &identityv1.GetUserResponse{User: f.user}, f.err
}

func TestIdentityUsersMapsTheRecord(t *testing.T) {
	f := &fakeIdentity{user: &identityv1.User{Id: "grace", Enabled: true, IsRoot: false, Roles: []string{"compliance-admin"},
		Groups: []string{"g-1"}, IdpGroups: []string{"Privacy Officers"}, Email: "grace@example.org", Name: "Grace Example"}}
	u, err := access.IdentityUsers(f).GetUser(context.Background(), "grace")
	require.NoError(t, err)
	require.Equal(t, []string{"grace"}, f.asked)
	require.Equal(t, access.User{ID: "grace", Enabled: true, Roles: []string{"compliance-admin"}, Groups: []string{"g-1"}, IdpGroups: []string{"Privacy Officers"}}, u)

	f.user = &identityv1.User{Id: "ivan", Enabled: true, DeletedAt: "2026-10-01T00:00:00Z", IsRoot: true}
	u, err = access.IdentityUsers(f).GetUser(context.Background(), "ivan")
	require.NoError(t, err)
	require.True(t, u.Deleted)
	require.True(t, u.Root)
}

func TestIdentityUsersTellsAnUnknownUserFromAnOutage(t *testing.T) {
	_, err := access.IdentityUsers(&fakeIdentity{err: status.Error(codes.NotFound, "no such user")}).GetUser(context.Background(), "x")
	require.ErrorIs(t, err, access.ErrNoUser)
	_, err = access.IdentityUsers(&fakeIdentity{err: status.Error(codes.Unavailable, "down")}).GetUser(context.Background(), "x")
	require.Error(t, err)
	require.False(t, errors.Is(err, access.ErrNoUser))
}
