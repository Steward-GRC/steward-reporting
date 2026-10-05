// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	apperr "github.com/Bugs5382/go-apperr"
	log "github.com/Bugs5382/go-log"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
	"github.com/Steward-GRC/steward-reporting/internal/store"
)

// maxNoticeTextLen bounds a notice's label and method, in characters.
const maxNoticeTextLen = 200

// Cases serves CaseService. Every call is the officer group's only.
type Cases struct {
	reportingv1.UnimplementedCaseServiceServer
	d Deps
}

// NewCases returns the officer-side service.
func NewCases(d Deps) *Cases { return &Cases{d: d} }

// officer returns the calling officer, or the coded refusal. A refusal of a
// known actor is audited.
func (s *Cases) officer(ctx context.Context, caseID string) (string, error) {
	who, err := signedIn(ctx)
	if isCode(err, errcodes.CodeActAsNotAllowed) {
		return "", s.refused(ctx, realUser(ctx), caseID, err)
	}
	if err != nil {
		return "", err
	}
	if err := s.d.Officers.Officer(ctx, who); err != nil {
		if isCode(err, errcodes.CodeCaseAccessDenied) {
			return "", s.refused(ctx, who, caseID, err)
		}
		return "", err
	}
	return who, nil
}

func (s *Cases) refused(ctx context.Context, who, caseID string, cause error) error {
	s.d.Log.Ctx(ctx).Warn("case call refused", log.F("user_id", who), log.F("case_id", caseID))
	if err := s.d.inTx(ctx, func(ctx context.Context) error {
		return s.d.emit(ctx, "case.access_refused", who, caseID, nil)
	}); err != nil {
		return err
	}
	return cause
}

func isCode(err error, code int) bool {
	c, ok := apperr.Code(err)
	return ok && c == code
}

// change runs fn on an open case, locked, with its audit event, and returns
// the case as it stands after.
func (s *Cases) change(ctx context.Context, op, officer, caseID, action string, attrs map[string]string, fn func(ctx context.Context) error) (store.Case, error) {
	var c store.Case
	err := s.d.inTx(ctx, func(ctx context.Context) error {
		if err := s.d.Store.LockOpen(ctx, caseID); err != nil {
			return err
		}
		if err := fn(ctx); err != nil {
			return err
		}
		if err := s.d.emit(ctx, action, officer, caseID, attrs); err != nil {
			return err
		}
		var err error
		c, err = s.d.Store.Get(ctx, caseID)
		return err
	})
	if err != nil {
		return store.Case{}, storeErr(op, err)
	}
	s.d.Log.Ctx(ctx).Info(action, log.F("case_id", caseID), log.F("officer_id", officer))
	return c, nil
}

// ListCases is the case queue.
func (s *Cases) ListCases(ctx context.Context, req *reportingv1.ListCasesRequest) (*reportingv1.ListCasesResponse, error) {
	officer, err := s.officer(ctx, "")
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	f := store.Filter{AssigneeUserID: req.GetAssigneeUserId()}
	for _, st := range req.GetStatuses() {
		d := statuses.dom(st)
		if d == "" {
			return nil, errcodes.Error(ctx, errcodes.Invalid("statuses"))
		}
		f.Statuses = append(f.Statuses, d)
	}
	var list []store.Summary
	var counts map[domain.Status]int
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if list, counts, err = s.d.Store.List(ctx, f); err != nil {
			return err
		}
		return s.d.emit(ctx, "case.listed", officer, "", map[string]string{"count": strconv.Itoa(len(list))})
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("list cases", err))
	}
	out := &reportingv1.ListCasesResponse{}
	for _, c := range list {
		out.Cases = append(out.Cases, &reportingv1.CaseSummary{
			Id: c.ID, CaseCode: c.CaseCode, Kind: kinds.proto(c.Kind), Status: statuses.proto(c.Status), Summary: c.Summary,
			AssigneeUserId: c.AssigneeUserID, ReceivedAt: ts(c.ReceivedAt), NextDeadline: c.NextDeadline,
		})
	}
	for _, st := range domain.Statuses() {
		out.Counts = append(out.Counts, &reportingv1.StatusCount{Status: statuses.proto(st), Count: int32(counts[st])}) // #nosec G115 -- a row count
	}
	return out, nil
}

// GetCase opens one case.
func (s *Cases) GetCase(ctx context.Context, req *reportingv1.GetCaseRequest) (*reportingv1.GetCaseResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var c store.Case
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if c, err = s.d.Store.Get(ctx, req.GetCaseId()); err != nil {
			return err
		}
		return s.d.emit(ctx, "case.viewed", officer, c.ID, nil)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("get case", err))
	}
	return &reportingv1.GetCaseResponse{Case: caseToProto(c)}, nil
}

// GetAttachment returns a stored attachment.
func (s *Cases) GetAttachment(ctx context.Context, req *reportingv1.GetAttachmentRequest) (*reportingv1.GetAttachmentResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var a store.Attachment
	var data []byte
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if a, data, err = s.d.Store.Attachment(ctx, req.GetCaseId(), req.GetAttachmentId()); err != nil {
			return err
		}
		return s.d.emit(ctx, "case.attachment_viewed", officer, req.GetCaseId(), map[string]string{"attachment_id": a.ID})
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("get attachment", err))
	}
	return &reportingv1.GetAttachmentResponse{
		Attachment: &reportingv1.Attachment{Id: a.ID, Filename: a.Filename, ContentType: a.ContentType, SizeBytes: a.Size, MetadataStripped: true},
		Data:       data,
	}, nil
}

// PostMessage writes to the reporter's thread.
func (s *Cases) PostMessage(ctx context.Context, req *reportingv1.PostMessageRequest) (*reportingv1.PostMessageResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	if err := domain.CheckBody(req.GetBody()); err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var m store.Message
	_, err = s.change(ctx, "post message", officer, req.GetCaseId(), "case.message_posted", nil, func(ctx context.Context) error {
		var err error
		if m, err = s.d.Store.AddMessage(ctx, req.GetCaseId(), store.AuthorOfficer, officer, req.GetBody()); err != nil {
			return err
		}
		return s.d.Store.SetStatus(ctx, req.GetCaseId(), domain.StatusNeedsReporterReply)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.PostMessageResponse{Message: messageToProto(m)}, nil
}

// AddNote adds an internal note.
func (s *Cases) AddNote(ctx context.Context, req *reportingv1.AddNoteRequest) (*reportingv1.AddNoteResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	if err := domain.CheckBody(req.GetBody()); err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var n store.Note
	_, err = s.change(ctx, "add note", officer, req.GetCaseId(), "case.note_added", nil, func(ctx context.Context) error {
		var err error
		n, err = s.d.Store.AddNote(ctx, req.GetCaseId(), officer, req.GetBody())
		return err
	})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.AddNoteResponse{Note: noteToProto(n)}, nil
}

// AssignCase sets or clears the assignee, who must be an officer.
func (s *Cases) AssignCase(ctx context.Context, req *reportingv1.AssignCaseRequest) (*reportingv1.AssignCaseResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	assignee := req.GetAssigneeUserId()
	if assignee != "" {
		if err := s.d.Officers.Officer(ctx, assignee); err != nil {
			if isCode(err, errcodes.CodeCaseAccessDenied) {
				err = errcodes.Invalid("assignee_user_id")
			}
			return nil, errcodes.Error(ctx, err)
		}
	}
	c, err := s.change(ctx, "assign case", officer, req.GetCaseId(), "case.assigned", map[string]string{"assignee_user_id": assignee},
		func(ctx context.Context) error { return s.d.Store.SetAssignee(ctx, req.GetCaseId(), assignee) })
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.AssignCaseResponse{Case: caseToProto(c)}, nil
}

// SetCaseStatus moves an open case.
func (s *Cases) SetCaseStatus(ctx context.Context, req *reportingv1.SetCaseStatusRequest) (*reportingv1.SetCaseStatusResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	st := statuses.dom(req.GetStatus())
	if err := domain.CheckSettableStatus(st); err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	c, err := s.change(ctx, "set status", officer, req.GetCaseId(), "case.status_changed", map[string]string{"status": string(st)},
		func(ctx context.Context) error { return s.d.Store.SetStatus(ctx, req.GetCaseId(), st) })
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.SetCaseStatusResponse{Case: caseToProto(c)}, nil
}

// SetDiscoveryDate sets when the incident was discovered.
func (s *Cases) SetDiscoveryDate(ctx context.Context, req *reportingv1.SetDiscoveryDateRequest) (*reportingv1.SetDiscoveryDateResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	d, err := domain.ParseDiscoveryDate(req.GetDiscoveredOn(), s.d.today())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	c, err := s.change(ctx, "set discovery date", officer, req.GetCaseId(), "case.discovery_date_set",
		map[string]string{"discovered_on": d.Format(domain.DateLayout)},
		func(ctx context.Context) error { return s.d.Store.SetDiscoveredOn(ctx, req.GetCaseId(), d) })
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.SetDiscoveryDateResponse{Case: caseToProto(c)}, nil
}

// RecordRiskAssessment records the assessment and the officer's decision.
func (s *Cases) RecordRiskAssessment(ctx context.Context, req *reportingv1.RecordRiskAssessmentRequest) (*reportingv1.RecordRiskAssessmentResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	f := factorsFromProto(req.GetFactors())
	decision := decisions.dom(req.GetDecision())
	if err := domain.CheckAssessment(f, decision, req.GetReason()); err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	a := store.Assessment{Factors: f, Suggestion: domain.Suggest(f), Decision: decision, Reason: strings.TrimSpace(req.GetReason()), DecidedBy: officer}
	next := domain.StatusInReview
	if decision == domain.DecisionReportable {
		next = domain.StatusNotificationDue
	}
	_, err = s.change(ctx, "record assessment", officer, req.GetCaseId(), "case.assessment_recorded",
		map[string]string{"decision": string(decision), "suggestion": string(a.Suggestion)},
		func(ctx context.Context) error {
			var err error
			if a, err = s.d.Store.AddAssessment(ctx, req.GetCaseId(), a); err != nil {
				return err
			}
			return s.d.Store.SetStatus(ctx, req.GetCaseId(), next)
		})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.RecordRiskAssessmentResponse{Assessment: assessmentToProto(a)}, nil
}

func checkNoticeText(field, v string) error {
	if utf8.RuneCountInString(v) > maxNoticeTextLen {
		return errcodes.Invalid(field)
	}
	return nil
}

// AddNotice adds a notice to track, with the days its recipient kind allows.
func (s *Cases) AddNotice(ctx context.Context, req *reportingv1.AddNoticeRequest) (*reportingv1.AddNoticeResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	r := noticeRecipients.dom(req.GetRecipient())
	for _, err := range []error{domain.CheckNoticeRecipient(r), checkNoticeText("label", req.GetLabel()), checkNoticeText("method", req.GetMethod())} {
		if err != nil {
			return nil, errcodes.Error(ctx, err)
		}
	}
	var n store.Notice
	c, err := s.change(ctx, "add notice", officer, req.GetCaseId(), "case.notice_added", map[string]string{"recipient": string(r)},
		func(ctx context.Context) error {
			var err error
			n, err = s.d.Store.AddNotice(ctx, req.GetCaseId(), store.Notice{Recipient: r, Label: strings.TrimSpace(req.GetLabel()),
				Method: strings.TrimSpace(req.GetMethod()), DaysAllowed: s.d.NoticeDays.For(r)})
			return err
		})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.AddNoticeResponse{Notice: noticeToProto(n, c.DiscoveredOn)}, nil
}

// UpdateNotice records a notice's progress.
func (s *Cases) UpdateNotice(ctx context.Context, req *reportingv1.UpdateNoticeRequest) (*reportingv1.UpdateNoticeResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	st := noticeStatuses.dom(req.GetStatus())
	sentOn, err := domain.CheckNoticeUpdate(st, req.GetSentOn(), s.d.today())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var n store.Notice
	c, err := s.change(ctx, "update notice", officer, req.GetCaseId(), "case.notice_updated",
		map[string]string{"notice_id": req.GetNoticeId(), "status": string(st)},
		func(ctx context.Context) error {
			var err error
			n, err = s.d.Store.UpdateNotice(ctx, req.GetCaseId(), req.GetNoticeId(), st, sentOn)
			return err
		})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.UpdateNoticeResponse{Notice: noticeToProto(n, c.DiscoveredOn)}, nil
}

// CloseCase records the outcome and corrective actions and posts the
// closing message to the reporter.
func (s *Cases) CloseCase(ctx context.Context, req *reportingv1.CloseCaseRequest) (*reportingv1.CloseCaseResponse, error) {
	officer, err := s.officer(ctx, req.GetCaseId())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	o := outcomes.dom(req.GetOutcome())
	actions := make([]domain.CorrectiveAction, 0, len(req.GetCorrectiveActions()))
	for _, a := range req.GetCorrectiveActions() {
		actions = append(actions, domain.CorrectiveAction{Description: strings.TrimSpace(a.GetDescription()), PolicyID: a.GetPolicyId()})
	}
	msg := strings.TrimSpace(req.GetClosingMessage())
	if err := domain.CheckCloseOut(o, actions, msg); err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	c, err := s.change(ctx, "close case", officer, req.GetCaseId(), "case.closed",
		map[string]string{"outcome": string(o), "corrective_actions": strconv.Itoa(len(actions))},
		func(ctx context.Context) error {
			if msg != "" {
				if _, err := s.d.Store.AddMessage(ctx, req.GetCaseId(), store.AuthorOfficer, officer, msg); err != nil {
					return err
				}
			}
			return s.d.Store.Close(ctx, req.GetCaseId(), o, actions)
		})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.CloseCaseResponse{Case: caseToProto(c)}, nil
}
