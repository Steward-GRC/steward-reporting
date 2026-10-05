// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"time"

	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

// DateLayout is the wire and storage form of a calendar date.
const DateLayout = time.DateOnly

// NoticeRecipient is who must be told.
type NoticeRecipient string

// The notice recipients.
const (
	NoticeAffectedPeople NoticeRecipient = "affected_people"
	NoticeRegulator      NoticeRecipient = "regulator"
	NoticeMedia          NoticeRecipient = "media"
	NoticeOther          NoticeRecipient = "other"
)

// NoticeStatus is where a notice stands.
type NoticeStatus string

// The notice statuses.
const (
	NoticeNotSent   NoticeStatus = "not_sent"
	NoticeDraft     NoticeStatus = "draft"
	NoticeSent      NoticeStatus = "sent"
	NoticeNotNeeded NoticeStatus = "not_needed"
)

// NoticeDays are the days allowed from discovery per recipient kind, from
// configuration.
type NoticeDays struct {
	Affected, Regulator, Media, Other int
}

// For returns the days allowed for r.
func (d NoticeDays) For(r NoticeRecipient) int {
	switch r {
	case NoticeAffectedPeople:
		return d.Affected
	case NoticeRegulator:
		return d.Regulator
	case NoticeMedia:
		return d.Media
	default:
		return d.Other
	}
}

// CheckNoticeRecipient checks a recipient kind.
func CheckNoticeRecipient(r NoticeRecipient) error {
	switch r {
	case NoticeAffectedPeople, NoticeRegulator, NoticeMedia, NoticeOther:
		return nil
	}
	return errcodes.Invalid("recipient")
}

// DueOn is the deadline: the discovery date plus days.
func DueOn(discovered time.Time, days int) string {
	return discovered.AddDate(0, 0, days).Format(DateLayout)
}

// ParseDiscoveryDate parses a discovery date, which can't be after today.
func ParseDiscoveryDate(s string, now time.Time) (time.Time, error) {
	d, err := time.Parse(DateLayout, s)
	if err != nil || d.After(now) {
		return time.Time{}, errcodes.Invalid("discovered_on")
	}
	return d, nil
}

// CheckNoticeUpdate checks a notice's new status and sent date, and returns
// the parsed sent date: required with NoticeSent and refused otherwise.
func CheckNoticeUpdate(s NoticeStatus, sentOn string, now time.Time) (*time.Time, error) {
	switch s {
	case NoticeSent:
		d, err := time.Parse(DateLayout, sentOn)
		if err != nil || d.After(now) {
			return nil, errcodes.Invalid("sent_on")
		}
		return &d, nil
	case NoticeNotSent, NoticeDraft, NoticeNotNeeded:
		if sentOn != "" {
			return nil, errcodes.Invalid("sent_on")
		}
		return nil, nil
	}
	return nil, errcodes.Invalid("status")
}
