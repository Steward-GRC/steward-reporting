// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

// Field length bounds, in characters.
const (
	MaxWhatHappenedLen = 10000
	MaxShortFieldLen   = 200
	MaxBodyLen         = 10000
	summaryLen         = 120
)

// InformationKind is a kind of information a concern involves.
type InformationKind string

// The information kinds.
const (
	InfoHealth    InformationKind = "health"
	InfoContact   InformationKind = "contact"
	InfoFinancial InformationKind = "financial"
	InfoNotSure   InformationKind = "not_sure"
)

var informationKinds = []InformationKind{InfoHealth, InfoContact, InfoFinancial, InfoNotSure}

// Answer is a yes, no or not sure reply; empty means unanswered.
type Answer string

// The answers.
const (
	AnswerYes     Answer = "yes"
	AnswerNo      Answer = "no"
	AnswerNotSure Answer = "not_sure"
)

// Details is what the reporter tells us. Nothing in it asks who they are.
type Details struct {
	WhatHappened     string
	Occurred         string
	Location         string
	InformationKinds []InformationKind
	StillHappening   Answer
}

// Normalize trims and checks the details, and drops repeated kinds.
func (d Details) Normalize() (Details, error) {
	d.WhatHappened = strings.TrimSpace(d.WhatHappened)
	d.Occurred = strings.TrimSpace(d.Occurred)
	d.Location = strings.TrimSpace(d.Location)
	switch {
	case d.WhatHappened == "" || utf8.RuneCountInString(d.WhatHappened) > MaxWhatHappenedLen:
		return Details{}, errcodes.Invalid("what_happened")
	case utf8.RuneCountInString(d.Occurred) > MaxShortFieldLen:
		return Details{}, errcodes.Invalid("occurred")
	case utf8.RuneCountInString(d.Location) > MaxShortFieldLen:
		return Details{}, errcodes.Invalid("location")
	}
	kinds, err := checkKinds(d.InformationKinds, "information_kinds")
	if err != nil {
		return Details{}, err
	}
	d.InformationKinds = kinds
	switch d.StillHappening {
	case "", AnswerYes, AnswerNo, AnswerNotSure:
	default:
		return Details{}, errcodes.Invalid("still_happening")
	}
	return d, nil
}

func checkKinds(in []InformationKind, name string) ([]InformationKind, error) {
	out := make([]InformationKind, 0, len(in))
	for _, k := range in {
		if !slices.Contains(informationKinds, k) {
			return nil, errcodes.Invalid(name)
		}
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out, nil
}

// Summary is the queue line for a report: its first line, cut to 120
// characters.
func Summary(whatHappened string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(whatHappened), "\n")
	line = strings.TrimSpace(line)
	if r := []rune(line); len(r) > summaryLen {
		return string(r[:summaryLen])
	}
	return line
}

// CheckBody checks a thread message or a note.
func CheckBody(body string) error {
	if strings.TrimSpace(body) == "" || utf8.RuneCountInString(body) > MaxBodyLen {
		return errcodes.Invalid("body")
	}
	return nil
}

// Status is where a case sits in the queue.
type Status string

// The statuses.
const (
	StatusNew                Status = "new"
	StatusInReview           Status = "in_review"
	StatusNeedsReporterReply Status = "needs_reporter_reply"
	StatusRiskAssessment     Status = "risk_assessment"
	StatusNotificationDue    Status = "notification_due"
	StatusClosed             Status = "closed"
)

// Statuses lists every status in queue order.
func Statuses() []Status {
	return []Status{StatusNew, StatusInReview, StatusNeedsReporterReply, StatusRiskAssessment, StatusNotificationDue, StatusClosed}
}

// CheckSettableStatus checks a status an officer sets by hand. Closing goes
// through close-out, which records the outcome.
func CheckSettableStatus(s Status) error {
	if s == StatusClosed || !slices.Contains(Statuses(), s) {
		return errcodes.Invalid("status")
	}
	return nil
}

// Kind says whether the reporter gave their name.
type Kind string

// The kinds.
const (
	KindAnonymous Kind = "anonymous"
	KindNamed     Kind = "named"
)
