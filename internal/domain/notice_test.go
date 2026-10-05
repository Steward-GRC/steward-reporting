// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
)

func day(s string) time.Time {
	d, err := time.Parse(domain.DateLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestDeadlinesCountFromDiscovery(t *testing.T) {
	require.Equal(t, "2026-12-01", domain.DueOn(day("2026-10-02"), 60))
	require.Equal(t, "2026-10-03", domain.DueOn(day("2026-10-02"), 1))
}

func TestNoticeDaysComeFromTheRecipientKind(t *testing.T) {
	days := domain.NoticeDays{Affected: 60, Regulator: 30, Media: 45, Other: 10}
	require.Equal(t, 60, days.For(domain.NoticeAffectedPeople))
	require.Equal(t, 30, days.For(domain.NoticeRegulator))
	require.Equal(t, 45, days.For(domain.NoticeMedia))
	require.Equal(t, 10, days.For(domain.NoticeOther))
}

func TestParseDate(t *testing.T) {
	now := day("2026-10-05")
	d, err := domain.ParseDiscoveryDate("2026-10-02", now)
	require.NoError(t, err)
	require.Equal(t, day("2026-10-02"), d)
	_, err = domain.ParseDiscoveryDate("2026-10-06", now)
	require.Equal(t, "discovered_on", field(t, err), "not in the future")
	_, err = domain.ParseDiscoveryDate("2 Oct 2026", now)
	require.Equal(t, "discovered_on", field(t, err))
}

func TestCheckNoticeUpdate(t *testing.T) {
	now := day("2026-10-05")
	sent, err := domain.CheckNoticeUpdate(domain.NoticeSent, "2026-10-04", now)
	require.NoError(t, err)
	require.NotNil(t, sent)
	_, err = domain.CheckNoticeUpdate(domain.NoticeSent, "", now)
	require.Equal(t, "sent_on", field(t, err))
	_, err = domain.CheckNoticeUpdate(domain.NoticeDraft, "2026-10-04", now)
	require.Equal(t, "sent_on", field(t, err))
	none, err := domain.CheckNoticeUpdate(domain.NoticeNotNeeded, "", now)
	require.NoError(t, err)
	require.Nil(t, none)
	_, err = domain.CheckNoticeUpdate("", "", now)
	require.Equal(t, "status", field(t, err))
}
