// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc

import (
	"context"
	"errors"
	"strconv"

	log "github.com/Bugs5382/go-log"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
	"github.com/Steward-GRC/steward-reporting/internal/store"
	"github.com/Steward-GRC/steward-reporting/internal/strip"
)

// MaxAttachments is the most files one report carries.
const MaxAttachments = 3

// caseCodeAttempts bounds the retries on a case code collision, which is
// already rare at 30^8 codes.
const caseCodeAttempts = 3

// Intake serves IntakeService.
type Intake struct {
	reportingv1.UnimplementedIntakeServiceServer
	d Deps
}

// NewIntake returns the reporter-side service.
func NewIntake(d Deps) *Intake { return &Intake{d: d} }

// prepareAttachments strips every attachment and gives it a neutral name.
func prepareAttachments(in []*reportingv1.AttachmentUpload) ([]store.NewAttachment, error) {
	if len(in) > MaxAttachments {
		return nil, errcodes.Invalid("attachments")
	}
	out := make([]store.NewAttachment, 0, len(in))
	for i, a := range in {
		res, err := strip.Strip(a.GetData())
		if errors.Is(err, strip.ErrUnsupported) {
			return nil, errcodes.AttachmentUnsupported(i + 1)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, store.NewAttachment{Filename: strip.NeutralName(i+1, res.ContentType), ContentType: res.ContentType, Data: res.Data})
	}
	return out, nil
}

// file stores a new case with a fresh case code and audits it.
func (s *Intake) file(ctx context.Context, nc store.NewCase) (id, code string, err error) {
	for range caseCodeAttempts {
		if nc.CaseCode, err = domain.NewCaseCode(); err != nil {
			return "", "", err
		}
		err = s.d.inTx(ctx, func(ctx context.Context) error {
			var err error
			if id, err = s.d.Store.Create(ctx, nc); err != nil {
				return err
			}
			return s.d.emit(ctx, "report.submitted", "", id, map[string]string{
				"kind": string(nc.Kind), "attachments": strconv.Itoa(len(nc.Attachments)),
			})
		})
		if !errors.Is(err, store.ErrCaseCodeTaken) {
			break
		}
		s.d.Log.Ctx(ctx).Warn("case code collision, drawing another")
	}
	if err != nil {
		return "", "", storeErr("file report", err)
	}
	return id, nc.CaseCode, nil
}

// GetIntakeOptions returns what the report form needs before anything is
// filed. It reads nothing about any report, so it isn't audited.
func (s *Intake) GetIntakeOptions(ctx context.Context, _ *reportingv1.GetIntakeOptionsRequest) (*reportingv1.GetIntakeOptionsResponse, error) {
	st, err := loadSettings(ctx, s.d.Store)
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.GetIntakeOptionsResponse{AnonymousReportsOpen: st.PublicLink, Categories: categoriesToProto(st.Categories)}, nil
}

// details checks the reported details against the settings: the category
// must be one of the configured ones.
func (s *Intake) details(ctx context.Context, in *reportingv1.ReportDetails) (domain.Details, domain.Settings, error) {
	details, err := detailsFromProto(in).Normalize()
	if err != nil {
		return domain.Details{}, domain.Settings{}, err
	}
	st, err := loadSettings(ctx, s.d.Store)
	if err != nil {
		return domain.Details{}, domain.Settings{}, err
	}
	if err := st.CheckCategory(details.Category); err != nil {
		return domain.Details{}, domain.Settings{}, err
	}
	return details, st, nil
}

// SubmitAnonymousReport files a report that keeps nobody's name, email or
// address. A forwarded actor is never read here. While the public link is
// off it is refused, before anything is stored.
func (s *Intake) SubmitAnonymousReport(ctx context.Context, req *reportingv1.SubmitAnonymousReportRequest) (*reportingv1.SubmitAnonymousReportResponse, error) {
	details, st, err := s.details(ctx, req.GetDetails())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	if !st.PublicLink {
		s.d.Log.Ctx(ctx).Info("anonymous report refused: the public link is off")
		return nil, errcodes.Error(ctx, errcodes.PublicLinkOff())
	}
	if err := domain.CheckPassphrase(req.GetPassphrase()); err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	atts, err := prepareAttachments(req.GetAttachments())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	hash, err := domain.HashPassphrase(req.GetPassphrase())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	id, code, err := s.file(ctx, store.NewCase{Kind: domain.KindAnonymous, PassphraseHash: hash, Details: details,
		DiscoveredOn: s.d.today(), ReceivedAt: s.d.now(), Attachments: atts})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	s.d.Log.Ctx(ctx).Info("anonymous report filed", log.F("case_id", id), log.F("attachments", len(atts)))
	return &reportingv1.SubmitAnonymousReportResponse{CaseCode: code}, nil
}

// openAnonymous finds the anonymous case a code and passphrase open. Every
// failure costs one passphrase hash and gives the same answer.
func (s *Intake) openAnonymous(ctx context.Context, rawCode, passphrase string) (string, error) {
	code, ok := domain.NormalizeCaseCode(rawCode)
	var id, hash string
	var err error
	if ok {
		id, hash, err = s.d.Store.FindAnonymous(ctx, code)
	}
	switch {
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return "", storeErr("find anonymous report", err)
	case ok && err == nil && domain.VerifyPassphrase(passphrase, hash):
		return id, nil
	case !ok || err != nil:
		domain.DummyVerify(passphrase)
	}
	s.d.Log.Ctx(ctx).Debug("a case code and passphrase opened nothing")
	if err := s.d.inTx(ctx, func(ctx context.Context) error { return s.d.emit(ctx, "report.check_refused", "", "", nil) }); err != nil {
		return "", err
	}
	return "", errcodes.ReportNotFound()
}

// CheckReport opens an anonymous report by its case code and passphrase.
func (s *Intake) CheckReport(ctx context.Context, req *reportingv1.CheckReportRequest) (*reportingv1.CheckReportResponse, error) {
	id, err := s.openAnonymous(ctx, req.GetCaseCode(), req.GetPassphrase())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var rc store.ReporterCase
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if rc, err = s.d.Store.GetForReporter(ctx, id); err != nil {
			return err
		}
		return s.d.emit(ctx, "report.checked", "", id, nil)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("check report", err))
	}
	return &reportingv1.CheckReportResponse{Report: reporterView(rc)}, nil
}

// reply adds the reporter's message and puts a case waiting on the reporter
// back in review.
func (s *Intake) reply(ctx context.Context, id, body, action string) (store.ReporterCase, error) {
	var rc store.ReporterCase
	err := s.d.inTx(ctx, func(ctx context.Context) error {
		if err := s.d.Store.LockOpen(ctx, id); err != nil {
			return err
		}
		if _, err := s.d.Store.AddMessage(ctx, id, store.AuthorReporter, "", body); err != nil {
			return err
		}
		current, err := s.d.Store.GetForReporter(ctx, id)
		if err != nil {
			return err
		}
		if current.Status == domain.StatusNeedsReporterReply {
			if err := s.d.Store.SetStatus(ctx, id, domain.StatusInReview); err != nil {
				return err
			}
		}
		if err := s.d.emit(ctx, action, "", id, nil); err != nil {
			return err
		}
		rc, err = s.d.Store.GetForReporter(ctx, id)
		return err
	})
	if err != nil {
		return store.ReporterCase{}, storeErr("reply", err)
	}
	s.d.Log.Ctx(ctx).Info("reporter replied", log.F("case_id", id))
	return rc, nil
}

// ReplyToReport adds the anonymous reporter's message.
func (s *Intake) ReplyToReport(ctx context.Context, req *reportingv1.ReplyToReportRequest) (*reportingv1.ReplyToReportResponse, error) {
	id, err := s.openAnonymous(ctx, req.GetCaseCode(), req.GetPassphrase())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	if err := domain.CheckBody(req.GetBody()); err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	rc, err := s.reply(ctx, id, req.GetBody(), "report.replied")
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.ReplyToReportResponse{Report: reporterView(rc)}, nil
}

// SubmitNamedReport files a report as the signed-in actor. The audit event
// doesn't name them: audit readers are not officers.
func (s *Intake) SubmitNamedReport(ctx context.Context, req *reportingv1.SubmitNamedReportRequest) (*reportingv1.SubmitNamedReportResponse, error) {
	reporter, err := signedIn(ctx)
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	details, _, err := s.details(ctx, req.GetDetails())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	atts, err := prepareAttachments(req.GetAttachments())
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	id, code, err := s.file(ctx, store.NewCase{Kind: domain.KindNamed, ReporterUserID: reporter, Details: details,
		DiscoveredOn: s.d.today(), ReceivedAt: s.d.now(), Attachments: atts})
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	s.d.Log.Ctx(ctx).Info("named report filed", log.F("case_id", id), log.F("attachments", len(atts)))
	return &reportingv1.SubmitNamedReportResponse{CaseId: id, CaseCode: code}, nil
}

// ListMyReports lists the signed-in actor's own named reports.
func (s *Intake) ListMyReports(ctx context.Context, _ *reportingv1.ListMyReportsRequest) (*reportingv1.ListMyReportsResponse, error) {
	reporter, err := signedIn(ctx)
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var list []store.ReporterCase
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if list, err = s.d.Store.ListByReporter(ctx, reporter); err != nil {
			return err
		}
		return s.d.emit(ctx, "report.listed", "", "", map[string]string{"count": strconv.Itoa(len(list))})
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("list own reports", err))
	}
	out := &reportingv1.ListMyReportsResponse{}
	for _, rc := range list {
		out.Reports = append(out.Reports, &reportingv1.MyReport{CaseId: rc.ID, Report: reporterView(rc)})
	}
	return out, nil
}

// GetMyReport opens one of the signed-in actor's own named reports.
func (s *Intake) GetMyReport(ctx context.Context, req *reportingv1.GetMyReportRequest) (*reportingv1.GetMyReportResponse, error) {
	reporter, err := signedIn(ctx)
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	var rc store.ReporterCase
	err = s.d.inTx(ctx, func(ctx context.Context) error {
		var err error
		if rc, err = s.d.Store.GetOwned(ctx, req.GetCaseId(), reporter); err != nil {
			return err
		}
		return s.d.emit(ctx, "report.viewed", "", rc.ID, nil)
	})
	if err != nil {
		return nil, errcodes.Error(ctx, storeErr("get own report", err))
	}
	return &reportingv1.GetMyReportResponse{Report: reporterView(rc)}, nil
}

// ReplyToMyReport adds the signed-in reporter's message.
func (s *Intake) ReplyToMyReport(ctx context.Context, req *reportingv1.ReplyToMyReportRequest) (*reportingv1.ReplyToMyReportResponse, error) {
	reporter, err := signedIn(ctx)
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	if err := domain.CheckBody(req.GetBody()); err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	if _, err := s.d.Store.GetOwned(ctx, req.GetCaseId(), reporter); err != nil {
		return nil, errcodes.Error(ctx, storeErr("get own report", err))
	}
	rc, err := s.reply(ctx, req.GetCaseId(), req.GetBody(), "report.replied")
	if err != nil {
		return nil, errcodes.Error(ctx, err)
	}
	return &reportingv1.ReplyToMyReportResponse{Report: reporterView(rc)}, nil
}
