// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package audit_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	auditv1 "github.com/Steward-GRC/steward-reporting/gen/go/thirdparty/audit/v1"
	"github.com/Steward-GRC/steward-reporting/internal/audit"
)

type published struct {
	key  string
	body []byte
}

type capture struct {
	got []published
	err error
}

func (c *capture) Publish(_ context.Context, key string, body []byte) error {
	c.got = append(c.got, published{key, body})
	return c.err
}

func TestEmitPublishesAuditsProtoEvent(t *testing.T) {
	c := &capture{}
	at := time.Date(2026, 10, 4, 9, 30, 0, 0, time.UTC)
	err := audit.New(c).Emit(context.Background(), audit.Event{
		Tier: audit.TierAudit, Action: "case.note_added", ActorUserID: "grace", Subject: "case:7b0e",
		GroupID: "group-1", Attributes: map[string]string{"decision": "reportable"}, OccurredAt: at,
	})
	require.NoError(t, err)
	require.Len(t, c.got, 1)
	require.Equal(t, "audit.audit", c.got[0].key)

	var ev auditv1.AuditEvent
	require.NoError(t, proto.Unmarshal(c.got[0].body, &ev))
	require.Equal(t, auditv1.Tier_TIER_AUDIT, ev.GetTier())
	require.Equal(t, "case.note_added", ev.GetAction())
	require.Equal(t, "grace", ev.GetActorUserId())
	require.Equal(t, "case:7b0e", ev.GetSubject())
	require.Equal(t, "group-1", ev.GetGroupId())
	require.Equal(t, map[string]string{"decision": "reportable"}, ev.GetAttributes())
	require.True(t, at.Equal(ev.GetOccurredAt().AsTime()))
}

func TestEmitStampsTheTimeAndRoutesTheActivityTier(t *testing.T) {
	c := &capture{}
	before := time.Now().UTC()
	require.NoError(t, audit.New(c).Emit(context.Background(), audit.Event{Tier: audit.TierActivity, Action: "case.viewed"}))
	require.Equal(t, "audit.activity", c.got[0].key)
	ev, err := audit.Decode(c.got[0].body)
	require.NoError(t, err)
	require.Equal(t, audit.TierActivity, ev.Tier)
	require.False(t, ev.OccurredAt.Before(before), "an unset time is stamped at emit")
}

func TestEmitRefusesAnEventWithoutATierOrAction(t *testing.T) {
	c := &capture{}
	require.Error(t, audit.New(c).Emit(context.Background(), audit.Event{Action: "case.viewed"}))
	require.Error(t, audit.New(c).Emit(context.Background(), audit.Event{Tier: audit.TierAudit}))
	require.Empty(t, c.got)
}

func TestEmitReturnsThePublishError(t *testing.T) {
	boom := errors.New("broker down")
	err := audit.New(&capture{err: boom}).Emit(context.Background(), audit.Event{Tier: audit.TierAudit, Action: "case.closed"})
	require.ErrorIs(t, err, boom)
}

func TestContentTypeNamesTheMessage(t *testing.T) {
	require.Equal(t, "application/protobuf; proto=steward.audit.v1.AuditEvent", audit.ContentType)
}
