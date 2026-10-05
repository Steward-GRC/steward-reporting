// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc_test

import (
	"testing"

	"github.com/Bugs5382/go-apperr/apperrgrpc"
	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"github.com/stretchr/testify/require"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

func TestAnonymousReportRoundTrip(t *testing.T) {
	e := newEnv(t)
	code, id := e.submitAnonymous(t)
	require.Regexp(t, `^[2-9A-Z]{3}-[2-9A-Z]{3}-[2-9A-Z]{2}$`, code)

	got, err := e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.NoError(t, err)
	require.Equal(t, reportingv1.CaseStatus_CASE_STATUS_NEW, got.GetReport().GetStatus())
	require.Equal(t, details().GetLocation(), got.GetReport().GetDetails().GetLocation())
	require.Equal(t, "2026-10-05", got.GetReport().GetReceivedAt().AsTime().Format("2006-01-02"))

	_, err = e.cases.PostMessage(as(t, grace), &reportingv1.PostMessageRequest{CaseId: id, Body: "Do you remember how many names?"})
	require.NoError(t, err)
	got, err = e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.NoError(t, err)
	require.Equal(t, reportingv1.CaseStatus_CASE_STATUS_NEEDS_REPORTER_REPLY, got.GetReport().GetStatus())
	require.Len(t, got.GetReport().GetThread(), 1)
	require.Equal(t, reportingv1.MessageAuthor_MESSAGE_AUTHOR_OFFICER, got.GetReport().GetThread()[0].GetAuthor())
	require.Empty(t, got.GetReport().GetThread()[0].GetOfficerUserId())

	replied, err := e.intake.ReplyToReport(t.Context(), &reportingv1.ReplyToReportRequest{CaseCode: code, Passphrase: passphrase, Body: "About one page, maybe 30 names."})
	require.NoError(t, err)
	require.Equal(t, reportingv1.CaseStatus_CASE_STATUS_IN_REVIEW, replied.GetReport().GetStatus(), "a reply puts the case back in review")
	require.Len(t, replied.GetReport().GetThread(), 2)
	require.Equal(t, reportingv1.MessageAuthor_MESSAGE_AUTHOR_REPORTER, replied.GetReport().GetThread()[1].GetAuthor())
}

func TestSubmitAnonymousReportValidates(t *testing.T) {
	e := newEnv(t)
	field := func(err error) string {
		t.Helper()
		info, ok := apperrgrpc.FromError(err)
		require.True(t, ok)
		require.Equal(t, errcodes.CodeInvalid, info.Code)
		return info.Metadata["field"]
	}
	_, err := e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: &reportingv1.ReportDetails{}, Passphrase: passphrase})
	require.Equal(t, "what_happened", field(err))
	_, err = e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: details(), Passphrase: "short"})
	require.Equal(t, "passphrase", field(err))
	four := make([]*reportingv1.AttachmentUpload, 4)
	for i := range four {
		four[i] = &reportingv1.AttachmentUpload{Filename: "note.txt", Data: []byte("note")}
	}
	_, err = e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: details(), Passphrase: passphrase, Attachments: four})
	require.Equal(t, "attachments", field(err))
}

func TestAttachmentsAreStrippedAndRenamed(t *testing.T) {
	e := newEnv(t)
	r, err := e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{
		Details: details(), Passphrase: passphrase,
		Attachments: []*reportingv1.AttachmentUpload{
			{Filename: "alice-example-at-her-desk.jpg", ContentType: "application/octet-stream", Data: photoWithExif(t)},
			{Filename: "what I saw.txt", ContentType: "text/plain", Data: []byte("The list had about 30 names.")},
		},
	})
	require.NoError(t, err)
	list, err := e.cases.ListCases(as(t, grace), &reportingv1.ListCasesRequest{})
	require.NoError(t, err)
	require.Equal(t, r.GetCaseCode(), list.GetCases()[0].GetCaseCode())
	c, err := e.cases.GetCase(as(t, grace), &reportingv1.GetCaseRequest{CaseId: list.GetCases()[0].GetId()})
	require.NoError(t, err)
	atts := c.GetCase().GetAttachments()
	require.Len(t, atts, 2)
	require.Equal(t, "attachment-1.jpg", atts[0].GetFilename())
	require.Equal(t, "image/jpeg", atts[0].GetContentType(), "the type is sniffed, not taken from the upload")
	require.True(t, atts[0].GetMetadataStripped())
	require.Equal(t, "attachment-2.txt", atts[1].GetFilename())

	got, err := e.cases.GetAttachment(as(t, grace), &reportingv1.GetAttachmentRequest{CaseId: c.GetCase().GetId(), AttachmentId: atts[0].GetId()})
	require.NoError(t, err)
	require.NotContains(t, string(got.GetData()), aliceName)
	require.NotContains(t, string(got.GetData()), "Exif")

	_, err = e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{
		Details: details(), Passphrase: passphrase,
		Attachments: []*reportingv1.AttachmentUpload{{Filename: "fine.txt", Data: []byte("ok")}, {Filename: "report.pdf", Data: []byte("%PDF-1.7 /Author (" + aliceName + ")")}},
	})
	info, ok := apperrgrpc.FromError(err)
	require.True(t, ok)
	require.Equal(t, errcodes.CodeAttachmentUnsupported, info.Code)
	require.Equal(t, "2", info.Metadata["attachment"])
}

func TestNamedReportsBelongToTheirReporter(t *testing.T) {
	e := newEnv(t)
	r, err := e.intake.SubmitNamedReport(as(t, alice), &reportingv1.SubmitNamedReportRequest{Details: details()})
	require.NoError(t, err)
	require.NotEmpty(t, r.GetCaseId())

	mine, err := e.intake.ListMyReports(as(t, alice), &reportingv1.ListMyReportsRequest{})
	require.NoError(t, err)
	require.Len(t, mine.GetReports(), 1)
	require.Equal(t, r.GetCaseCode(), mine.GetReports()[0].GetReport().GetCaseCode())
	theirs, err := e.intake.ListMyReports(as(t, bob), &reportingv1.ListMyReportsRequest{})
	require.NoError(t, err)
	require.Empty(t, theirs.GetReports())

	_, err = e.intake.GetMyReport(as(t, bob), &reportingv1.GetMyReportRequest{CaseId: r.GetCaseId()})
	require.Equal(t, errcodes.CodeCaseNotFound, errCode(t, err))
	got, err := e.intake.ReplyToMyReport(as(t, alice), &reportingv1.ReplyToMyReportRequest{CaseId: r.GetCaseId(), Body: "One more thing."})
	require.NoError(t, err)
	require.Len(t, got.GetReport().GetThread(), 1)

	c, err := e.cases.GetCase(as(t, grace), &reportingv1.GetCaseRequest{CaseId: r.GetCaseId()})
	require.NoError(t, err)
	require.Equal(t, reportingv1.ReportKind_REPORT_KIND_NAMED, c.GetCase().GetKind())
	require.Equal(t, alice, c.GetCase().GetReporterUserId(), "officers see who reported")
}

func TestNamedReportsNeedASignedInReporter(t *testing.T) {
	e := newEnv(t)
	_, err := e.intake.SubmitNamedReport(t.Context(), &reportingv1.SubmitNamedReportRequest{Details: details()})
	require.Equal(t, errcodes.CodeSignInRequired, errCode(t, err))
	_, err = e.intake.ListMyReports(t.Context(), &reportingv1.ListMyReportsRequest{})
	require.Equal(t, errcodes.CodeSignInRequired, errCode(t, err))
	actingAs := grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: alice, Impersonator: dave})
	_, err = e.intake.SubmitNamedReport(actingAs, &reportingv1.SubmitNamedReportRequest{Details: details()})
	require.Equal(t, errcodes.CodeActAsNotAllowed, errCode(t, err), "nobody files or reads a report as someone else")
	_, err = e.intake.ListMyReports(actingAs, &reportingv1.ListMyReportsRequest{})
	require.Equal(t, errcodes.CodeActAsNotAllowed, errCode(t, err))
}

func TestAClosedCaseTakesNoReply(t *testing.T) {
	e := newEnv(t)
	code, id := e.submitAnonymous(t)
	_, err := e.cases.CloseCase(as(t, grace), &reportingv1.CloseCaseRequest{CaseId: id, Outcome: reportingv1.Outcome_OUTCOME_INCONCLUSIVE})
	require.NoError(t, err)
	_, err = e.intake.ReplyToReport(t.Context(), &reportingv1.ReplyToReportRequest{CaseCode: code, Passphrase: passphrase, Body: "hello"})
	require.Equal(t, errcodes.CodeCaseClosed, errCode(t, err))
	got, err := e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.NoError(t, err, "a closed report can still be checked")
	require.Equal(t, reportingv1.CaseStatus_CASE_STATUS_CLOSED, got.GetReport().GetStatus())
}
