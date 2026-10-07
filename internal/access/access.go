// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package access decides who may work cases: the members of the configured
// officer groups, and nobody else. Membership comes from identity, never from
// the caller, and is decided through steward-authz's rule engine with one
// group rule per officer group. Only a matching rule admits: the engine's
// built-in reads for site admins and root, which open the policy library, do
// not open cases.
package access

import (
	"context"
	"errors"
	"slices"

	stewardauthz "github.com/Steward-GRC/steward-authz"

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
	// Groups are the ids of the local groups the user is in.
	Groups []string
	// IdpGroups are the group names the identity provider asserted.
	IdpGroups []string
}

// Users looks users up in identity.
type Users interface {
	GetUser(ctx context.Context, id string) (User, error)
}

// officerRuleset names the rule set the officer groups are compiled into.
const officerRuleset = "reporting.officers"

// GroupSource returns the officer groups as they stand now.
type GroupSource func(ctx context.Context) ([]string, error)

// Checker decides officer and settings access.
type Checker struct {
	users  Users
	groups GroupSource
}

// New returns a checker for a fixed set of officer groups: local group ids or
// identity provider group names, matched ignoring case. No groups means no
// officers.
func New(users Users, officerGroups []string) *Checker {
	return NewFromSource(users, func(context.Context) ([]string, error) { return officerGroups, nil })
}

// NewFromSource returns a checker that reads the officer groups on every
// call, so a change to them applies to the next call.
func NewFromSource(users Users, groups GroupSource) *Checker {
	return &Checker{users: users, groups: groups}
}

func compile(officerGroups []string) *stewardauthz.Evaluator {
	rules := make([]stewardauthz.Rule, 0, len(officerGroups))
	for _, g := range officerGroups {
		rules = append(rules, stewardauthz.Rule{
			Subject: stewardauthz.RuleSubject{Kind: stewardauthz.SubjectGroup, Name: g},
			Grants:  map[stewardauthz.Action]stewardauthz.Grant{stewardauthz.ActionRead: stewardauthz.GrantAllow, stewardauthz.ActionAuthor: stewardauthz.GrantAllow},
		})
	}
	return stewardauthz.Compile([]stewardauthz.CategoryRuleset{{Name: officerRuleset, Rules: rules}})
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

func roles(u User) []stewardauthz.Role {
	var out []stewardauthz.Role
	for _, r := range u.Roles {
		if role, err := stewardauthz.ParseRole(r); err == nil {
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
	groups, err := c.groups(ctx)
	if err != nil {
		return err
	}
	subject := stewardauthz.Subject{
		UserID: u.ID,
		Groups: append(append([]string{}, u.Groups...), u.IdpGroups...),
		Root:   u.Root,
		Roles:  roles(u),
	}
	res := compile(groups).Resolve(ctx, subject)
	if res.Read.Reason != stewardauthz.ReasonRuleAllow || res.Author.Reason != stewardauthz.ReasonRuleAllow {
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
	if u.Root || slices.Contains(stewardauthz.RolePermissions(roles(u)...), stewardauthz.ComplianceManage) {
		return nil
	}
	return errcodes.SettingsAccessDenied()
}
