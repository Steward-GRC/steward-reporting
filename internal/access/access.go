// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package access decides who may work cases: the holders of the
// privacy-officer role in identity, and nobody else. The role comes from
// identity on every call, never from the caller. Only the role admits: site
// admins and root, who read the policy library, do not open cases. Category
// read and author decisions for report scoping come from go-authz's ordered
// engine (category.go) and role permissions from its rbac catalog (internal/authz).
package access

import (
	"context"
	"errors"
	"slices"

	"github.com/Steward-GRC/steward-reporting/internal/authz"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

// ErrNoUser means identity has no such user.
var ErrNoUser = errors.New("access: no such user")

// User is identity's view of a user, as far as access needs it.
type User struct {
	ID      string
	Enabled bool
	Deleted bool
	Root    bool
	Roles   []string
	// Groups are the ids of the local groups the user is in and the group
	// names the identity provider asserted.
	Groups []string
}

// Users looks users up in identity.
type Users interface {
	GetUser(ctx context.Context, id string) (User, error)
}

// PrivacyOfficerRole is the identity role that admits a user to cases.
const PrivacyOfficerRole = "privacy-officer"

// Checker decides officer and settings access.
type Checker struct {
	users Users
}

// New returns a checker that asks users who holds which role.
func New(users Users) *Checker {
	return &Checker{users: users}
}

// lookup returns the enabled, undeleted user, or denied when there is none.
func (c *Checker) lookup(ctx context.Context, userID string, denied func() error) (User, error) {
	u, err := c.users.GetUser(ctx, userID)
	if errors.Is(err, ErrNoUser) {
		return User{}, denied()
	}
	if err != nil {
		return User{}, errcodes.IdentityUnavailable(err)
	}
	if !u.Enabled || u.Deleted {
		return User{}, denied()
	}
	return u, nil
}

func roles(u User) []authz.Role {
	var out []authz.Role
	for _, r := range u.Roles {
		if role, err := authz.ParseRole(r); err == nil {
			out = append(out, role)
		}
	}
	return out
}

// Officer returns nil when userID may work cases, CaseAccessDenied when not,
// and IdentityUnavailable when identity can't say.
func (c *Checker) Officer(ctx context.Context, userID string) error {
	u, err := c.lookup(ctx, userID, errcodes.CaseAccessDenied)
	if err != nil {
		return err
	}
	if !slices.Contains(u.Roles, PrivacyOfficerRole) {
		return errcodes.CaseAccessDenied()
	}
	return nil
}

// SettingsAdmin returns nil when userID may read and change the Compliance
// settings: a holder of compliance.manage (compliance admins, and site admins
// through the catalog's wildcard) or root. SettingsAccessDenied when not, and
// IdentityUnavailable when identity can't say.
func (c *Checker) SettingsAdmin(ctx context.Context, userID string) error {
	u, err := c.lookup(ctx, userID, errcodes.SettingsAccessDenied)
	if err != nil {
		return err
	}
	if u.Root || slices.Contains(authz.RolePermissions(roles(u)...), authz.ComplianceManage) {
		return nil
	}
	return errcodes.SettingsAccessDenied()
}

// Root returns nil when userID is the enabled, undeleted root in identity,
// HardResetRootRequired when not, and IdentityUnavailable when identity can't
// say. Identity decides root; a site admin or compliance admin is not root.
func (c *Checker) Root(ctx context.Context, userID string) error {
	u, err := c.lookup(ctx, userID, errcodes.HardResetRootRequired)
	if err != nil {
		return err
	}
	if !u.Root {
		return errcodes.HardResetRootRequired()
	}
	return nil
}
