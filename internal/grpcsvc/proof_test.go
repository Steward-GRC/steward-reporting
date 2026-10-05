// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	grpcactor "github.com/Bugs5382/go-grpc-actor"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/errcodes"
)

const reporterIP = "198.51.100.23"

// photoWithExif is a JPEG whose EXIF segment names Alice, her phone and
// where she stood.
func photoWithExif(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{200, 10, 10, 255})
	var plain bytes.Buffer
	require.NoError(t, jpeg.Encode(&plain, img, nil))
	payload := []byte("Exif\x00\x00" + aliceName + " " + aliceEmail + " GPS 40.7128N 74.0060W")
	seg := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	return append(append(append([]byte{}, plain.Bytes()[:2]...), append(seg, payload...)...), plain.Bytes()[2:]...)
}

// everything returns every row of every table, the attachment bytes and the
// queued audit events, as text.
func everything(t *testing.T, e *env) string {
	t.Helper()
	ctx := context.Background()
	q := e.db.Querier()
	rows, err := q.Query(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`)
	require.NoError(t, err)
	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	rows.Close()
	require.Contains(t, tables, "cases")
	require.Contains(t, tables, auditTable)
	var b strings.Builder
	for _, table := range tables {
		rows, err := q.Query(ctx, `SELECT row_to_json(t)::text FROM "`+table+`" t`)
		require.NoError(t, err)
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			b.WriteString(line + "\n")
		}
		rows.Close()
	}
	for _, query := range []string{`SELECT data FROM case_attachments`, `SELECT payload FROM ` + auditTable} {
		rows, err := q.Query(ctx, query)
		require.NoError(t, err)
		for rows.Next() {
			var raw []byte
			require.NoError(t, rows.Scan(&raw))
			b.Write(raw)
			b.WriteString("\n")
		}
		rows.Close()
	}
	return b.String()
}

// Proof 1: an anonymous report stores no name, email or address. Alice is
// signed in when she chooses to report anonymously, the call carries her
// address in forwarding headers, and her photo's file name and EXIF name
// her; none of it is kept, logged or audited, and identity is never asked.
func TestProofAnAnonymousReportStoresNoNameEmailOrAddress(t *testing.T) {
	e := newEnv(t)
	ctx := grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: alice, Session: "session-alice"})
	ctx = metadata.AppendToOutgoingContext(ctx, "x-forwarded-for", reporterIP, "x-real-ip", reporterIP, "forwarded", "for="+reporterIP)
	callsBefore := e.users.calls.Load()

	r, err := e.intake.SubmitAnonymousReport(ctx, &reportingv1.SubmitAnonymousReportRequest{
		Details: details(), Passphrase: passphrase,
		Attachments: []*reportingv1.AttachmentUpload{{Filename: "alice-example-at-her-desk.jpg", ContentType: "image/jpeg", Data: photoWithExif(t)}},
	})
	require.NoError(t, err)
	require.Equal(t, callsBefore, e.users.calls.Load(), "identity is never asked about an anonymous reporter")

	_, err = e.intake.CheckReport(ctx, &reportingv1.CheckReportRequest{CaseCode: r.GetCaseCode(), Passphrase: passphrase})
	require.NoError(t, err)
	_, err = e.intake.ReplyToReport(ctx, &reportingv1.ReplyToReportRequest{CaseCode: r.GetCaseCode(), Passphrase: passphrase, Body: "About one page, maybe 30 names."})
	require.NoError(t, err)

	stored := everything(t, e)
	require.Contains(t, stored, r.GetCaseCode(), "the scan does see the report")
	logs := e.logs.String()
	for _, secret := range []string{alice, "session-alice", aliceName, aliceEmail, "alice-example", reporterIP, "127.0.0.1", passphrase} {
		require.NotContains(t, stored, secret, "stored: %s", secret)
		require.NotContains(t, logs, secret, "logged: %s", secret)
	}

	var reporter *string
	require.NoError(t, e.db.Querier().QueryRow(context.Background(), `SELECT reporter_user_id FROM cases`).Scan(&reporter))
	require.Nil(t, reporter)
	for _, ev := range e.events(t) {
		require.Empty(t, ev.ActorUserID, "%s names no actor", ev.Action)
	}
}

// Proof 2: the case code and passphrase are the only way back in.
func TestProofTheCaseCodeAndPassphraseAreTheOnlyWayBackIn(t *testing.T) {
	e := newEnv(t)
	signedIn := as(t, alice)
	r, err := e.intake.SubmitAnonymousReport(signedIn, &reportingv1.SubmitAnonymousReportRequest{Details: details(), Passphrase: passphrase})
	require.NoError(t, err)
	code := r.GetCaseCode()
	other, err := e.intake.SubmitAnonymousReport(t.Context(), &reportingv1.SubmitAnonymousReportRequest{Details: details(), Passphrase: "another passphrase"})
	require.NoError(t, err)

	got, err := e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: strings.ToLower(code), Passphrase: passphrase})
	require.NoError(t, err)
	require.Equal(t, code, got.GetReport().GetCaseCode(), "the right code and passphrase open it, typed any case")

	tries := []*reportingv1.CheckReportRequest{
		{CaseCode: code},
		{Passphrase: passphrase},
		{CaseCode: code, Passphrase: "correct horse battery!"},
		{CaseCode: code, Passphrase: strings.ToUpper(passphrase)},
		{CaseCode: other.GetCaseCode(), Passphrase: passphrase},
		{CaseCode: "222-222-22", Passphrase: passphrase},
		{CaseCode: "not a code", Passphrase: passphrase},
	}
	for _, try := range tries {
		_, err := e.intake.CheckReport(signedIn, try)
		require.Equal(t, codes.NotFound, status.Code(err), "%v", try)
		require.Equal(t, errcodes.CodeReportNotFound, errCode(t, err), "one answer for every wrong try")
		_, err = e.intake.ReplyToReport(signedIn, &reportingv1.ReplyToReportRequest{CaseCode: try.GetCaseCode(), Passphrase: try.GetPassphrase(), Body: "hello"})
		require.Equal(t, errcodes.CodeReportNotFound, errCode(t, err))
	}

	mine, err := e.intake.ListMyReports(signedIn, &reportingv1.ListMyReportsRequest{})
	require.NoError(t, err)
	require.Empty(t, mine.GetReports(), "the signed-in user who filed it can't list it")

	var id string
	require.NoError(t, e.db.Querier().QueryRow(context.Background(), `SELECT id FROM cases WHERE case_code = $1`, code).Scan(&id))
	_, err = e.intake.GetMyReport(signedIn, &reportingv1.GetMyReportRequest{CaseId: id})
	require.Equal(t, errcodes.CodeCaseNotFound, errCode(t, err), "nor open it by id")
	_, err = e.intake.ReplyToMyReport(signedIn, &reportingv1.ReplyToMyReportRequest{CaseId: id, Body: "hello"})
	require.Equal(t, errcodes.CodeCaseNotFound, errCode(t, err))
	_, err = e.cases.GetCase(signedIn, &reportingv1.GetCaseRequest{CaseId: id})
	require.Equal(t, errcodes.CodeCaseAccessDenied, errCode(t, err), "nor through the officer side")
}

type outsider struct {
	name string
	ctx  context.Context
	code int
}

func outsiders(t *testing.T) []outsider {
	return []outsider{
		{"no actor", t.Context(), errcodes.CodeSignInRequired},
		{"a reader", as(t, bob), errcodes.CodeCaseAccessDenied},
		{"the reporter", as(t, alice), errcodes.CodeCaseAccessDenied},
		{"a compliance admin", as(t, carol), errcodes.CodeCaseAccessDenied},
		{"a site admin", as(t, dave), errcodes.CodeCaseAccessDenied},
		{"root", as(t, root), errcodes.CodeCaseAccessDenied},
		{"a site admin acting as an officer", grpcactor.WithActor(t.Context(), grpcactor.Actor{Subject: grace, Impersonator: dave}), errcodes.CodeActAsNotAllowed},
	}
}

// Proof 3: nothing in a case reaches anyone outside the officer group.
func TestProofNothingInACaseReachesAnyoneOutsideTheOfficerGroup(t *testing.T) {
	e := newEnv(t)
	code, id := e.submitAnonymous(t)
	officer := as(t, grace)
	const (
		note    = "Print room has no badge release. Raise with Facilities."
		message = "Thank you. Do you remember roughly how many names were on the list?"
		reason  = "Unattended for about 2 hours in a shared room."
		method  = "Letter to each person on the list"
	)
	_, err := e.cases.AddNote(officer, &reportingv1.AddNoteRequest{CaseId: id, Body: note})
	require.NoError(t, err)
	_, err = e.cases.PostMessage(officer, &reportingv1.PostMessageRequest{CaseId: id, Body: message})
	require.NoError(t, err)
	_, err = e.cases.RecordRiskAssessment(officer, &reportingv1.RecordRiskAssessmentRequest{CaseId: id, Factors: factors(), Decision: reportingv1.BreachDecision_BREACH_DECISION_REPORTABLE, Reason: reason})
	require.NoError(t, err)
	_, err = e.cases.AddNotice(officer, &reportingv1.AddNoticeRequest{CaseId: id, Recipient: reportingv1.NoticeRecipient_NOTICE_RECIPIENT_AFFECTED_PEOPLE, Method: method})
	require.NoError(t, err)
	_, err = e.cases.AssignCase(officer, &reportingv1.AssignCaseRequest{CaseId: id, AssigneeUserId: heidi})
	require.NoError(t, err)

	sd := reportingv1.File_steward_reporting_v1_cases_proto.Services().Get(0)
	for i := range sd.Methods().Len() {
		m := sd.Methods().Get(i)
		full := "/" + string(sd.FullName()) + "/" + string(m.Name())
		for _, o := range outsiders(t) {
			req := dynamicpb.NewMessage(m.Input())
			if f := m.Input().Fields().ByName("case_id"); f != nil {
				req.Set(f, protoreflect.ValueOfString(id))
			}
			resp := dynamicpb.NewMessage(m.Output())
			err := e.conn.Invoke(o.ctx, full, req, resp)
			require.Error(t, err, "%s calling %s", o.name, m.Name())
			require.Contains(t, []codes.Code{codes.Unauthenticated, codes.PermissionDenied}, status.Code(err), "%s calling %s", o.name, m.Name())
			require.Equal(t, o.code, errCode(t, err), "%s calling %s", o.name, m.Name())
		}
	}

	view, err := e.intake.CheckReport(t.Context(), &reportingv1.CheckReportRequest{CaseCode: code, Passphrase: passphrase})
	require.NoError(t, err)
	seen := prototext.Format(view)
	require.Contains(t, seen, message, "the reporter reads the officer's question")
	for _, hidden := range []string{note, reason, method, grace, heidi} {
		require.NotContains(t, seen, hidden, "the reporter's view never carries %q", hidden)
	}

	text := details().GetWhatHappened()
	refused := 0
	for _, ev := range e.events(t) {
		blob := ev.Subject + " " + ev.Action
		for k, v := range ev.Attributes {
			blob += " " + k + "=" + v
		}
		for _, content := range []string{text, "Patient list", note, message, reason, method, code} {
			require.NotContains(t, blob, content, "audit readers are not officers: %s", ev.Action)
		}
		if ev.Action == "case.access_refused" {
			refused++
		}
	}
	require.Equal(t, sd.Methods().Len()*(len(outsiders(t))-1), refused, "every refused officer call with an actor is audited")
}
