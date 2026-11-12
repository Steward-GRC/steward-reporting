// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package access_test

import (
	"context"
	"testing"

	"github.com/Bugs5382/go-authz/ordered"
	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/access"
)

const (
	allow = ordered.Allow
	deny  = ordered.Deny
)

var team = access.Level{CategoryID: "team", Rules: []access.Rule{
	{Subject: "group:officers", Read: allow},
}}

func level(id string, owners []string, rules ...access.Rule) access.Level {
	return access.Level{CategoryID: id, Owners: owners, Rules: rules}
}

func decide(t *testing.T, userID string, a access.Action, levels ...access.Level) bool {
	t.Helper()
	ok, err := access.New(directory).CategoryAllowed(context.Background(), userID, levels, a)
	require.NoError(t, err)
	return ok
}

// A cell answers only its own action: a Read allow gives no Author, Approve or
// Manage.
func TestACellAnswersOnlyItsOwnAction(t *testing.T) {
	require.True(t, decide(t, "grace", access.ActionRead, team))
	for _, a := range []access.Action{access.ActionAuthor, access.ActionApprove, access.ActionManage, access.ActionAck} {
		require.False(t, decide(t, "grace", a, team), a)
	}
}

// The first non-blank cell per action wins, action by action: an earlier row
// that is blank for an action does not stop a later row from deciding it.
func TestFirstNonBlankCellPerActionWins(t *testing.T) {
	l := level("c", nil,
		access.Rule{Subject: "group:officers", Read: allow},
		access.Rule{Subject: "everyone:*", Read: deny, Author: deny},
	)
	require.True(t, decide(t, "grace", access.ActionRead, l), "the first row's Read allow wins")
	require.False(t, decide(t, "grace", access.ActionAuthor, l), "the first row is blank for Author, so the Everyone deny decides")
	require.False(t, decide(t, "alice", access.ActionRead, l))
}

// Manage implies Read and Author, even over an explicit deny of those cells.
func TestManageImpliesReadAndAuthor(t *testing.T) {
	l := level("c", nil,
		access.Rule{Subject: "group:officers", Manage: allow},
		access.Rule{Subject: "everyone:*", Read: deny, Author: deny},
	)
	require.True(t, decide(t, "grace", access.ActionManage, l))
	require.True(t, decide(t, "grace", access.ActionRead, l))
	require.True(t, decide(t, "grace", access.ActionAuthor, l))
	require.False(t, decide(t, "alice", access.ActionRead, l))
}

// A child with no rows of its own inherits its parent's rows.
func TestChildrenInherit(t *testing.T) {
	child := level("child", nil)
	require.True(t, decide(t, "grace", access.ActionRead, child, team))
	require.False(t, decide(t, "alice", access.ActionRead, child, team))
	own := level("child", nil, access.Rule{Subject: "group:officers", Read: deny})
	require.False(t, decide(t, "grace", access.ActionRead, own, team), "the child's own row comes first")
}

// Owners always Read, Author, Approve and Manage, whatever any row says, and
// never Ack.
func TestOwnersAlwaysHaveTheirActions(t *testing.T) {
	l := level("c", []string{"user:alice"}, access.Rule{Subject: "everyone:*", Read: deny, Author: deny, Approve: deny, Manage: deny})
	for _, a := range []access.Action{access.ActionRead, access.ActionAuthor, access.ActionApprove, access.ActionManage} {
		require.True(t, decide(t, "alice", a, l), a)
	}
	require.False(t, decide(t, "alice", access.ActionAck, l), "ownership gives no Ack; only a row does")
	require.False(t, decide(t, "carol", access.ActionRead, l))
}

// A non-root site admin follows the rules like anyone else: a deny for the
// site-admin role applies, and nothing admits them on its own.
func TestNonRootSiteAdminFollowsTheRules(t *testing.T) {
	l := level("c", nil,
		access.Rule{Subject: "role:site-admin", Read: deny, Author: deny},
		access.Rule{Subject: "everyone:*", Read: allow, Author: allow},
	)
	require.False(t, decide(t, "dave", access.ActionRead, l))
	require.False(t, decide(t, "dave", access.ActionAuthor, l))
	require.True(t, decide(t, "alice", access.ActionRead, l))
	require.False(t, decide(t, "dave", access.ActionRead, level("empty", nil)), "no rule, no access")
}

// Root is exempt from the rows for Read, Author and Manage only: a deny against
// it is stored but never applied.
func TestRootIsExemptForReadAuthorAndManage(t *testing.T) {
	l := level("c", nil,
		access.Rule{Subject: "role:root", Read: deny, Author: deny, Manage: deny},
		access.Rule{Subject: "everyone:*", Read: deny, Author: deny, Manage: deny},
	)
	for _, a := range []access.Action{access.ActionRead, access.ActionAuthor, access.ActionManage} {
		require.True(t, decide(t, "root", a, l), a)
	}
	require.True(t, decide(t, "root", access.ActionRead, level("empty", nil)), "no rule at all, still Read")
	require.False(t, decide(t, "alice", access.ActionRead, l))
}

// Approve and Ack are not global for root: the rows decide.
func TestRootApproveAndAckFollowTheRows(t *testing.T) {
	denied := level("c", nil, access.Rule{Subject: "everyone:*", Read: allow, Approve: deny, Ack: deny})
	granted := level("c", nil, access.Rule{Subject: "everyone:*", Read: allow, Approve: allow, Ack: allow})
	rootDenied := level("c", nil, access.Rule{Subject: "role:root", Read: allow, Approve: deny, Ack: deny}, access.Rule{Subject: "everyone:*", Approve: allow, Ack: allow})
	for _, a := range []access.Action{access.ActionApprove, access.ActionAck} {
		require.False(t, decide(t, "root", a, denied), string(a)+" deny applies")
		require.False(t, decide(t, "root", a, rootDenied), string(a)+" deny against root applies")
		require.False(t, decide(t, "root", a, level("c", nil)), string(a)+" no row")
		require.True(t, decide(t, "root", a, granted), string(a)+" allow applies")
	}
}

// Ack requires Read: an Ack allow is not enough when Read is denied.
func TestAckRequiresRead(t *testing.T) {
	l := level("c", nil, access.Rule{Subject: "everyone:*", Read: deny, Ack: allow})
	require.False(t, decide(t, "alice", access.ActionAck, l))
	l = level("c", nil, access.Rule{Subject: "everyone:*", Read: allow, Ack: allow})
	require.True(t, decide(t, "alice", access.ActionAck, l))
}

func TestDisabledAndUnknownUsersAreRefused(t *testing.T) {
	l := level("c", nil, access.Rule{Subject: "everyone:*", Read: allow})
	for _, id := range []string{"ivan", "deleted", "nobody"} {
		require.False(t, decide(t, id, access.ActionRead, l), id)
	}
	_, err := access.New(directory).CategoryAllowed(context.Background(), "broken", []access.Level{l}, access.ActionRead)
	require.Error(t, err)
}

// A report scope holds only the categories the viewer may Read.
func TestReadableCategoriesKeepsOnlyWhatTheViewerMayRead(t *testing.T) {
	open := []access.Level{level("open", nil, access.Rule{Subject: "everyone:*", Read: allow})}
	officers := []access.Level{team}
	closed := []access.Level{level("closed", nil, access.Rule{Subject: "everyone:*", Read: deny})}
	chains := [][]access.Level{open, officers, closed}
	ctx := context.Background()
	c := access.New(directory)
	for user, want := range map[string][]string{
		"alice": {"open"},
		"grace": {"open", "team"},
		"dave":  {"open"},
		"root":  {"open", "team", "closed"},
	} {
		got, err := c.ReadableCategories(ctx, user, chains)
		require.NoError(t, err)
		require.Equal(t, want, got, user)
	}
}
