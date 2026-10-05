// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

// enum pairs one domain value with its proto enum.
type enum[D ~string, P comparable] struct {
	toProto map[D]P
	toDom   map[P]D
}

func newEnum[D ~string, P comparable](pairs map[D]P) enum[D, P] {
	e := enum[D, P]{toProto: pairs, toDom: make(map[P]D, len(pairs))}
	for d, p := range pairs {
		e.toDom[p] = d
	}
	return e
}

// dom returns the domain value; an unknown or unspecified enum is empty, which
// the domain checks refuse.
func (e enum[D, P]) dom(p P) D   { return e.toDom[p] }
func (e enum[D, P]) proto(d D) P { return e.toProto[d] }

var (
	statuses = newEnum(map[domain.Status]reportingv1.CaseStatus{
		domain.StatusNew:                reportingv1.CaseStatus_CASE_STATUS_NEW,
		domain.StatusInReview:           reportingv1.CaseStatus_CASE_STATUS_IN_REVIEW,
		domain.StatusNeedsReporterReply: reportingv1.CaseStatus_CASE_STATUS_NEEDS_REPORTER_REPLY,
		domain.StatusRiskAssessment:     reportingv1.CaseStatus_CASE_STATUS_RISK_ASSESSMENT,
		domain.StatusNotificationDue:    reportingv1.CaseStatus_CASE_STATUS_NOTIFICATION_DUE,
		domain.StatusClosed:             reportingv1.CaseStatus_CASE_STATUS_CLOSED,
	})
	kinds = newEnum(map[domain.Kind]reportingv1.ReportKind{
		domain.KindAnonymous: reportingv1.ReportKind_REPORT_KIND_ANONYMOUS,
		domain.KindNamed:     reportingv1.ReportKind_REPORT_KIND_NAMED,
	})
	infoKinds = newEnum(map[domain.InformationKind]reportingv1.InformationKind{
		domain.InfoHealth:    reportingv1.InformationKind_INFORMATION_KIND_HEALTH,
		domain.InfoContact:   reportingv1.InformationKind_INFORMATION_KIND_CONTACT,
		domain.InfoFinancial: reportingv1.InformationKind_INFORMATION_KIND_FINANCIAL,
		domain.InfoNotSure:   reportingv1.InformationKind_INFORMATION_KIND_NOT_SURE,
	})
	answers = newEnum(map[domain.Answer]reportingv1.Answer{
		domain.AnswerYes:     reportingv1.Answer_ANSWER_YES,
		domain.AnswerNo:      reportingv1.Answer_ANSWER_NO,
		domain.AnswerNotSure: reportingv1.Answer_ANSWER_NOT_SURE,
	})
	authors = newEnum(map[store.Author]reportingv1.MessageAuthor{
		store.AuthorReporter: reportingv1.MessageAuthor_MESSAGE_AUTHOR_REPORTER,
		store.AuthorOfficer:  reportingv1.MessageAuthor_MESSAGE_AUTHOR_OFFICER,
	})
	recipients = newEnum(map[domain.Recipient]reportingv1.Recipient{
		domain.RecipientStaffOnly:           reportingv1.Recipient_RECIPIENT_STAFF_ONLY,
		domain.RecipientUnknownPeople:       reportingv1.Recipient_RECIPIENT_UNKNOWN_PEOPLE,
		domain.RecipientAnotherOrganisation: reportingv1.Recipient_RECIPIENT_ANOTHER_ORGANISATION,
	})
	viewed = newEnum(map[domain.Viewed]reportingv1.Viewed{
		domain.ViewedYes:      reportingv1.Viewed_VIEWED_YES,
		domain.ViewedProbably: reportingv1.Viewed_VIEWED_PROBABLY,
		domain.ViewedNo:       reportingv1.Viewed_VIEWED_NO,
	})
	mitigations = newEnum(map[domain.Mitigation]reportingv1.Mitigation{
		domain.MitigationFully:    reportingv1.Mitigation_MITIGATION_FULLY,
		domain.MitigationPartly:   reportingv1.Mitigation_MITIGATION_PARTLY,
		domain.MitigationNotAtAll: reportingv1.Mitigation_MITIGATION_NOT_AT_ALL,
	})
	suggestions = newEnum(map[domain.Suggestion]reportingv1.Suggestion{
		domain.SuggestNotify:  reportingv1.Suggestion_SUGGESTION_NOTIFICATION_LIKELY_REQUIRED,
		domain.SuggestLowRisk: reportingv1.Suggestion_SUGGESTION_LOW_PROBABILITY_OF_COMPROMISE,
	})
	decisions = newEnum(map[domain.Decision]reportingv1.BreachDecision{
		domain.DecisionReportable:    reportingv1.BreachDecision_BREACH_DECISION_REPORTABLE,
		domain.DecisionNotReportable: reportingv1.BreachDecision_BREACH_DECISION_NOT_REPORTABLE,
	})
	noticeRecipients = newEnum(map[domain.NoticeRecipient]reportingv1.NoticeRecipient{
		domain.NoticeAffectedPeople: reportingv1.NoticeRecipient_NOTICE_RECIPIENT_AFFECTED_PEOPLE,
		domain.NoticeRegulator:      reportingv1.NoticeRecipient_NOTICE_RECIPIENT_REGULATOR,
		domain.NoticeMedia:          reportingv1.NoticeRecipient_NOTICE_RECIPIENT_MEDIA,
		domain.NoticeOther:          reportingv1.NoticeRecipient_NOTICE_RECIPIENT_OTHER,
	})
	noticeStatuses = newEnum(map[domain.NoticeStatus]reportingv1.NoticeStatus{
		domain.NoticeNotSent:   reportingv1.NoticeStatus_NOTICE_STATUS_NOT_SENT,
		domain.NoticeDraft:     reportingv1.NoticeStatus_NOTICE_STATUS_DRAFT,
		domain.NoticeSent:      reportingv1.NoticeStatus_NOTICE_STATUS_SENT,
		domain.NoticeNotNeeded: reportingv1.NoticeStatus_NOTICE_STATUS_NOT_NEEDED,
	})
	outcomes = newEnum(map[domain.Outcome]reportingv1.Outcome{
		domain.OutcomeSubstantiated:    reportingv1.Outcome_OUTCOME_SUBSTANTIATED,
		domain.OutcomeNotSubstantiated: reportingv1.Outcome_OUTCOME_NOT_SUBSTANTIATED,
		domain.OutcomeInconclusive:     reportingv1.Outcome_OUTCOME_INCONCLUSIVE,
	})
)

// unknown is a value no domain check accepts, for an enum the caller sent
// that this build doesn't know.
const unknown = "unknown"

func infoFromProto(in []reportingv1.InformationKind) []domain.InformationKind {
	out := make([]domain.InformationKind, len(in))
	for i, k := range in {
		if out[i] = infoKinds.dom(k); out[i] == "" {
			out[i] = unknown
		}
	}
	return out
}

func infoToProto(in []domain.InformationKind) []reportingv1.InformationKind {
	out := make([]reportingv1.InformationKind, len(in))
	for i, k := range in {
		out[i] = infoKinds.proto(k)
	}
	return out
}

func detailsFromProto(d *reportingv1.ReportDetails) domain.Details {
	still := answers.dom(d.GetStillHappening())
	if still == "" && d.GetStillHappening() != reportingv1.Answer_ANSWER_UNSPECIFIED {
		still = unknown
	}
	return domain.Details{
		WhatHappened: d.GetWhatHappened(), Occurred: d.GetOccurred(), Location: d.GetLocation(),
		InformationKinds: infoFromProto(d.GetInformationKinds()), StillHappening: still,
	}
}

func detailsToProto(d domain.Details) *reportingv1.ReportDetails {
	return &reportingv1.ReportDetails{
		WhatHappened: d.WhatHappened, Occurred: d.Occurred, Location: d.Location,
		InformationKinds: infoToProto(d.InformationKinds), StillHappening: answers.proto(d.StillHappening),
	}
}

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func messageToProto(m store.Message) *reportingv1.ThreadMessage {
	return &reportingv1.ThreadMessage{Id: m.ID, Author: authors.proto(m.Author), OfficerUserId: m.OfficerUserID, Body: m.Body, CreatedAt: ts(m.CreatedAt)}
}

func reporterView(rc store.ReporterCase) *reportingv1.ReporterView {
	v := &reportingv1.ReporterView{CaseCode: rc.CaseCode, Status: statuses.proto(rc.Status), Details: detailsToProto(rc.Details), ReceivedAt: ts(rc.ReceivedAt)}
	for _, m := range rc.Thread {
		m.OfficerUserID = ""
		v.Thread = append(v.Thread, messageToProto(m))
	}
	return v
}

func noteToProto(n store.Note) *reportingv1.Note {
	return &reportingv1.Note{Id: n.ID, AuthorUserId: n.AuthorUserID, Body: n.Body, CreatedAt: ts(n.CreatedAt)}
}

func factorsFromProto(f *reportingv1.RiskFactors) domain.Factors {
	out := domain.Factors{
		Information: infoFromProto(f.GetInformation()), Recipient: recipients.dom(f.GetRecipient()),
		Viewed: viewed.dom(f.GetViewed()), Mitigation: mitigations.dom(f.GetMitigation()),
	}
	return out
}

func assessmentToProto(a store.Assessment) *reportingv1.RiskAssessment {
	return &reportingv1.RiskAssessment{
		Factors: &reportingv1.RiskFactors{
			Information: infoToProto(a.Factors.Information), Recipient: recipients.proto(a.Factors.Recipient),
			Viewed: viewed.proto(a.Factors.Viewed), Mitigation: mitigations.proto(a.Factors.Mitigation),
		},
		Suggestion: suggestions.proto(a.Suggestion), Decision: decisions.proto(a.Decision), Reason: a.Reason,
		DecidedByUserId: a.DecidedBy, DecidedAt: ts(a.DecidedAt),
	}
}

func noticeToProto(n store.Notice, discovered time.Time) *reportingv1.Notice {
	out := &reportingv1.Notice{
		Id: n.ID, Recipient: noticeRecipients.proto(n.Recipient), Label: n.Label, Method: n.Method,
		DaysAllowed: int32(n.DaysAllowed), // #nosec G115 -- days come from validated configuration
		DueOn:       domain.DueOn(discovered, n.DaysAllowed), Status: noticeStatuses.proto(n.Status),
	}
	if n.SentOn != nil {
		out.SentOn = n.SentOn.Format(domain.DateLayout)
	}
	return out
}

func caseToProto(c store.Case) *reportingv1.Case {
	out := &reportingv1.Case{
		Id: c.ID, CaseCode: c.CaseCode, Kind: kinds.proto(c.Kind), Status: statuses.proto(c.Status), Details: detailsToProto(c.Details),
		ReporterUserId: c.ReporterUserID, AssigneeUserId: c.AssigneeUserID, ReceivedAt: ts(c.ReceivedAt),
		DiscoveredOn: c.DiscoveredOn.Format(domain.DateLayout), Outcome: outcomes.proto(c.Outcome),
	}
	if c.ClosedAt != nil {
		out.ClosedAt = ts(*c.ClosedAt)
	}
	for _, a := range c.Attachments {
		out.Attachments = append(out.Attachments, &reportingv1.Attachment{Id: a.ID, Filename: a.Filename, ContentType: a.ContentType, SizeBytes: a.Size, MetadataStripped: true})
	}
	for _, m := range c.Thread {
		out.Thread = append(out.Thread, messageToProto(m))
	}
	for _, n := range c.Notes {
		out.Notes = append(out.Notes, noteToProto(n))
	}
	if c.Assessment != nil {
		out.Assessment = assessmentToProto(*c.Assessment)
	}
	for _, n := range c.Notices {
		out.Notices = append(out.Notices, noticeToProto(n, c.DiscoveredOn))
	}
	for _, a := range c.CorrectiveActions {
		out.CorrectiveActions = append(out.CorrectiveActions, &reportingv1.CorrectiveAction{Description: a.Description, PolicyId: a.PolicyID})
	}
	return out
}
