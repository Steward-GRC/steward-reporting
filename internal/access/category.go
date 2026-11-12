// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package access

import (
	"context"
	"errors"

	"github.com/Bugs5382/go-authz/ordered"
)

// Action is one of the five category actions.
type Action string

// The category actions of the ordered model.
const (
	ActionRead    Action = "read"
	ActionAuthor  Action = "author"
	ActionApprove Action = "approve"
	ActionManage  Action = "manage"
	ActionAck     Action = "ack"
)

// errUnusable marks a user who is unknown, disabled or deleted: no access, and
// not an error for the caller.
var errUnusable = errors.New("access: user unusable")

func unusable() error { return errUnusable }

// rootRole is the seat that is exempt from the rows.
const rootRole = "root"

// Rule is one access row of a category: a subject ("user:<id>", "group:<id>",
// "role:<name>" or "everyone:*") and a cell per action. A blank cell is skipped
// and a cell answers only its own action. This is the shape core's
// GetEffectiveRuleset returns for each rule.
type Rule struct {
	Subject                            string
	Read, Author, Approve, Manage, Ack ordered.Effect
}

// Level is one category in a chain with its owners (subjects such as
// "user:<id>") and its rules in order.
type Level struct {
	CategoryID string
	Owners     []string
	Rules      []Rule
}

// categoryModel is the ordered model with root as the exempt seat for Read,
// Author and Manage only; Approve and Ack follow the rows for root too.
// Non-root site admins follow the rows for every action.
func categoryModel() ordered.Model {
	m := ordered.DefaultModel()
	m.Superusers = &ordered.Superusers{
		Subjects: []string{"role:" + rootRole},
		Actions:  []string{string(ActionRead), string(ActionAuthor), string(ActionManage)},
	}
	return m
}

// ruleSet turns a chain (the category first, then each ancestor) into a rule
// set. Each non-blank cell is one entry, in rule order.
func ruleSet(levels []Level) ordered.RuleSet {
	rs := ordered.RuleSet{Categories: map[string]ordered.Category{}, Parents: map[string]string{}}
	for i, l := range levels {
		cat := ordered.Category{Owners: l.Owners}
		for _, r := range l.Rules {
			for _, cell := range []struct {
				action Action
				effect ordered.Effect
			}{
				{ActionRead, r.Read}, {ActionAuthor, r.Author}, {ActionApprove, r.Approve}, {ActionManage, r.Manage}, {ActionAck, r.Ack},
			} {
				if cell.effect != "" {
					cat.Entries = append(cat.Entries, ordered.Entry{Subject: r.Subject, Action: string(cell.action), Effect: cell.effect})
				}
			}
		}
		rs.Categories[l.CategoryID] = cat
		if i+1 < len(levels) {
			rs.Parents[l.CategoryID] = levels[i+1].CategoryID
		}
	}
	return rs
}

func (u User) person() ordered.Person {
	p := ordered.Person{ID: u.ID, Groups: u.Groups}
	for _, r := range roles(u) {
		p.Roles = append(p.Roles, string(r))
	}
	if u.Root {
		p.Roles = append(p.Roles, rootRole)
	}
	return p
}

// CategoryAllowed reports whether userID may do the action in the category at
// the head of levels (the category first, then each ancestor up to the top).
// Disabled, deleted and unknown users are refused; IdentityUnavailable when
// identity can't say.
func (c *Checker) CategoryAllowed(ctx context.Context, userID string, levels []Level, action Action) (bool, error) {
	if len(levels) == 0 {
		return false, nil
	}
	u, err := c.lookup(ctx, userID, unusable)
	if err != nil {
		if errors.Is(err, errUnusable) {
			return false, nil
		}
		return false, err
	}
	return categoryModel().Decide(ruleSet(levels), u.person(), levels[0].CategoryID, string(action)).Allowed, nil
}

// ReadableCategories returns the ids of the categories, one chain each, that
// userID may Read, in the order given. A report scope holds only these.
func (c *Checker) ReadableCategories(ctx context.Context, userID string, chains [][]Level) ([]string, error) {
	u, err := c.lookup(ctx, userID, unusable)
	if err != nil {
		if errors.Is(err, errUnusable) {
			return []string{}, nil
		}
		return nil, err
	}
	m, p := categoryModel(), u.person()
	out := []string{}
	for _, levels := range chains {
		if len(levels) > 0 && m.Decide(ruleSet(levels), p, levels[0].CategoryID, string(ActionRead)).Allowed {
			out = append(out, levels[0].CategoryID)
		}
	}
	return out, nil
}
