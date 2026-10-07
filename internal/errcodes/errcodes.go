// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package errcodes holds the reporting service's coded errors (8100 to 8199)
// and turns them into gRPC statuses through go-apperr.
package errcodes

import (
	"context"
	"errors"
	"strconv"
	"sync"

	apperr "github.com/Bugs5382/go-apperr"
	"github.com/Bugs5382/go-apperr/apperrgrpc"
	log "github.com/Bugs5382/go-log"
)

// Domain is the ErrorInfo domain every reporting error carries.
const Domain = "reporting"

// The reporting service's codes.
const (
	CodeInternal              = 8100
	CodeStoreUnavailable      = 8101
	CodeInvalid               = 8102
	CodeReportNotFound        = 8103
	CodeSignInRequired        = 8104
	CodeCaseAccessDenied      = 8105
	CodeCaseNotFound          = 8106
	CodeCaseClosed            = 8107
	CodeAttachmentUnsupported = 8108
	CodeIdentityUnavailable   = 8109
	CodeActAsNotAllowed       = 8110
	CodeSettingsAccessDenied  = 8111
	CodePublicLinkOff         = 8112
)

// Entries returns the registry entries.
func Entries() []apperr.Entry {
	return []apperr.Entry{
		{Code: CodeInternal, Symbol: "INTERNAL", Category: apperr.CategoryInternal,
			Title: "reporting", Cause: "an uncoded failure inside the reporting service"},
		{Code: CodeStoreUnavailable, Symbol: "REPORT_STORE_UNAVAILABLE", Category: apperr.CategoryInternal,
			Title: "reporting store", Cause: "a reporting Postgres read or write failed; the op metadata names it, the cause is only logged"},
		{Code: CodeInvalid, Symbol: "REPORT_INVALID", Category: apperr.CategoryInvalid,
			Title: "report or case input", Cause: "a request field is missing, too long or out of range; field names it",
			UserSafe: true, Message: "Something in this form isn't valid. Check it and try again."},
		{Code: CodeReportNotFound, Symbol: "REPORT_NOT_FOUND", Category: apperr.CategoryNotFound,
			Title: "check a report", Cause: "no anonymous report matches the case code and passphrase; an unknown code and a wrong passphrase are not told apart",
			UserSafe: true, Message: "No report matches that case code and passphrase."},
		{Code: CodeSignInRequired, Symbol: "REPORT_SIGN_IN_REQUIRED", Category: apperr.CategoryUnauthenticated,
			Title: "named reports and cases", Cause: "a named-report or case call ran without a trusted forwarded actor",
			UserSafe: true, Message: "You need to be signed in to do this. You can still report anonymously without signing in."},
		{Code: CodeCaseAccessDenied, Symbol: "CASE_ACCESS_DENIED", Category: apperr.CategoryPermissionDenied,
			Title: "cases", Cause: "the actor is not in an officer group; site admins and root are not officers unless they are in one",
			UserSafe: true, Message: "Only privacy officers can open cases."},
		{Code: CodeCaseNotFound, Symbol: "CASE_NOT_FOUND", Category: apperr.CategoryNotFound,
			Title: "cases", Cause: "no case or no attachment, notice or own named report with that id",
			UserSafe: true, Message: "That case doesn't exist."},
		{Code: CodeCaseClosed, Symbol: "CASE_CLOSED", Category: apperr.CategoryFailedPrecondition,
			Title: "cases", Cause: "a change or a reply was sent to a closed case",
			UserSafe: true, Message: "This case is closed and can't be changed."},
		{Code: CodeAttachmentUnsupported, Symbol: "ATTACHMENT_UNSUPPORTED", Category: apperr.CategoryInvalid,
			Title: "attachments", Cause: "an attachment is not a JPEG, PNG, GIF or plain text file, can't be decoded, or is too large; attachment is its 1-based number",
			UserSafe: true, Message: "That file can't be attached. Attach a JPEG, PNG or GIF image or a plain text file, up to 10 MB."},
		{Code: CodeIdentityUnavailable, Symbol: "IDENTITY_UNAVAILABLE", Category: apperr.CategoryUnavailable,
			Title: "officer check", Cause: "identity could not be asked whether the actor is an officer, so the call is refused"},
		{Code: CodeActAsNotAllowed, Symbol: "ACT_AS_NOT_ALLOWED", Category: apperr.CategoryPermissionDenied,
			Title: "act-as", Cause: "an impersonated actor called a case or named-report RPC; act-as never reaches cases or reports",
			UserSafe: true, Message: "Cases and reports can't be opened while acting as another user."},
		{Code: CodeSettingsAccessDenied, Symbol: "SETTINGS_ACCESS_DENIED", Category: apperr.CategoryPermissionDenied,
			Title: "compliance settings", Cause: "the actor holds neither compliance.manage nor root",
			UserSafe: true, Message: "Only compliance admins can see or change the Compliance settings."},
		{Code: CodePublicLinkOff, Symbol: "PUBLIC_LINK_OFF", Category: apperr.CategoryFailedPrecondition,
			Title: "anonymous reports", Cause: "the public report link is switched off in the Compliance settings, so new anonymous reports are refused",
			UserSafe: true, Message: "Anonymous reporting is switched off. Sign in to report a concern with your name."},
	}
}

var (
	regOnce sync.Once
	reg     *apperr.Registry
)

// Registry returns the service registry. Coded errors are logged through
// go-log with the trace of the request they failed.
func Registry() *apperr.Registry {
	regOnce.Do(func() {
		r, err := apperr.NewRegistry(Entries(), apperr.WithService(81), apperr.WithCodeDigits(4),
			apperr.WithLogger(logSink{log.NewLogger("reporting")}))
		if err != nil {
			panic(err)
		}
		reg = r
	})
	return reg
}

// Error turns err into the gRPC error a handler returns.
func Error(ctx context.Context, err error) error {
	return apperrgrpc.Error(ctx, Registry(), err, CodeInternal, Domain)
}

// Doc is the Markdown body of docs/error-codes.md.
func Doc() string {
	return "# Error codes\n\nEvery coded gRPC error from the reporting service carries an `ErrorInfo` with the symbol as\n" +
		"its reason, the domain `" + Domain + "` and the code in `codeNum`. Only user-safe messages reach\n" +
		"the caller; every other code is sent as `Code N: Internal Error`.\n\n" + Registry().Markdown()
}

var (
	errInvalid       = errors.New("reporting: invalid input")
	errReport        = errors.New("reporting: no report matches")
	errNoActor       = errors.New("reporting: no trusted forwarded actor")
	errNotOfficer    = errors.New("reporting: the actor is not an officer")
	errNoCase        = errors.New("reporting: no such case")
	errClosed        = errors.New("reporting: the case is closed")
	errAttachment    = errors.New("reporting: unsupported attachment")
	errImpersonating = errors.New("reporting: act-as is not allowed")
	errNotAdmin      = errors.New("reporting: the actor may not manage the compliance settings")
	errPublicLinkOff = errors.New("reporting: the public report link is off")
)

// StoreUnavailable codes a failed store call; op names it.
func StoreUnavailable(op string, cause error) error {
	return apperr.WithMeta(apperr.Coded(CodeStoreUnavailable, cause), apperr.Meta("op", op))
}

// Invalid codes an invalid request field.
func Invalid(field string) error {
	return apperr.WithMeta(apperr.Coded(CodeInvalid, errInvalid), apperr.Meta("field", field))
}

// ReportNotFound codes a case code and passphrase that open nothing.
func ReportNotFound() error { return apperr.Coded(CodeReportNotFound, errReport) }

// SignInRequired codes a call that needs a forwarded actor and has none.
func SignInRequired() error { return apperr.Coded(CodeSignInRequired, errNoActor) }

// CaseAccessDenied codes an actor outside the officer group.
func CaseAccessDenied() error { return apperr.Coded(CodeCaseAccessDenied, errNotOfficer) }

// CaseNotFound codes a missing case, attachment, notice or own report.
func CaseNotFound() error { return apperr.Coded(CodeCaseNotFound, errNoCase) }

// CaseClosed codes a change to a closed case.
func CaseClosed() error { return apperr.Coded(CodeCaseClosed, errClosed) }

// AttachmentUnsupported codes a refused attachment; n is its 1-based number.
func AttachmentUnsupported(n int) error {
	return apperr.WithMeta(apperr.Coded(CodeAttachmentUnsupported, errAttachment), apperr.Meta("attachment", strconv.Itoa(n)))
}

// IdentityUnavailable codes an officer check identity couldn't answer.
func IdentityUnavailable(cause error) error { return apperr.Coded(CodeIdentityUnavailable, cause) }

// ActAsNotAllowed codes an impersonated actor on a case or report call.
func ActAsNotAllowed() error { return apperr.Coded(CodeActAsNotAllowed, errImpersonating) }

// SettingsAccessDenied codes an actor who may not manage the settings.
func SettingsAccessDenied() error { return apperr.Coded(CodeSettingsAccessDenied, errNotAdmin) }

// PublicLinkOff codes a new anonymous report while the public link is off.
func PublicLinkOff() error { return apperr.Coded(CodePublicLinkOff, errPublicLinkOff) }

type logSink struct{ l log.Logger }

func (s logSink) LogCoded(ctx context.Context, code int, err error) {
	s.l.Ctx(ctx).Debug("coded error", log.F("code", code), log.F("error", err.Error()))
}
