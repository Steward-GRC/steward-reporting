// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package access

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/Steward-GRC/steward-reporting/gen/go/thirdparty/identity/v1"
)

// IdentityClient is the one identity call access makes.
type IdentityClient interface {
	GetUser(ctx context.Context, in *identityv1.GetUserRequest, opts ...grpc.CallOption) (*identityv1.GetUserResponse, error)
}

// IdentityUsers reads users from identity's IdentityReadService. Only what
// the officer decision needs is kept; names and emails are dropped here.
func IdentityUsers(c IdentityClient) Users { return identityUsers{c} }

type identityUsers struct{ c IdentityClient }

func (i identityUsers) GetUser(ctx context.Context, id string) (User, error) {
	resp, err := i.c.GetUser(ctx, &identityv1.GetUserRequest{UserId: id})
	if status.Code(err) == codes.NotFound {
		return User{}, ErrNoUser
	}
	if err != nil {
		return User{}, fmt.Errorf("access: identity GetUser: %w", err)
	}
	u := resp.GetUser()
	return User{
		ID: u.GetId(), Enabled: u.GetEnabled(), Deleted: u.GetDeletedAt() != "", Root: u.GetIsRoot(),
		Roles: u.GetRoles(), Groups: u.GetGroups(), IdpGroups: u.GetIdpGroups(),
	}, nil
}
