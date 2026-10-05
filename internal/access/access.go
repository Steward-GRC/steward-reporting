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

// Checker decides officer access.
type Checker struct {
	users     Users
	evaluator *stewardauthz.Evaluator
}

// New returns a checker for the given officer groups: local group ids or
// identity provider group names, matched ignoring case. No groups means no
// officers.
func New(users Users, officerGroups []string) *Checker {
	rules := make([]stewardauthz.Rule, 0, len(officerGroups))
	for _, g := range officerGroups {
		rules = append(rules, stewardauthz.Rule{
			Subject: stewardauthz.RuleSubject{Kind: stewardauthz.SubjectGroup, Name: g},
			Grants:  map[stewardauthz.Action]stewardauthz.Grant{stewardauthz.ActionRead: stewardauthz.GrantAllow, stewardauthz.ActionAuthor: stewardauthz.GrantAllow},
		})
	}
	chain := []stewardauthz.CategoryRuleset{{Name: officerRuleset, Rules: rules}}
	return &Checker{users: users, evaluator: stewardauthz.Compile(chain)}
}

// Officer returns nil when userID may work cases, CaseAccessDenied when not,
// and IdentityUnavailable when identity can't say.
func (c *Checker) Officer(ctx context.Context, userID string) error {
	u, err := c.users.GetUser(ctx, userID)
	if errors.Is(err, ErrNoUser) {
		return errcodes.CaseAccessDenied()
	}
	if err != nil {
		return errcodes.IdentityUnavailable(err)
	}
	if !u.Enabled || u.Deleted {
		return errcodes.CaseAccessDenied()
	}
	subject := stewardauthz.Subject{
		UserID: u.ID,
		Groups: append(append([]string{}, u.Groups...), u.IdpGroups...),
		Root:   u.Root,
	}
	for _, r := range u.Roles {
		if role, err := stewardauthz.ParseRole(r); err == nil {
			subject.Roles = append(subject.Roles, role)
		}
	}
	res := c.evaluator.Resolve(ctx, subject)
	if res.Read.Reason != stewardauthz.ReasonRuleAllow || res.Author.Reason != stewardauthz.ReasonRuleAllow {
		return errcodes.CaseAccessDenied()
	}
	return nil
}
