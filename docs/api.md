# API

Two gRPC services in `proto/steward/reporting/v1`. The gateway is the only caller.

## IntakeService: the reporter's side

| RPC | Who | What |
| --- | --- | --- |
| `SubmitAnonymousReport` | anyone | Files a report with no name, email or address and returns the one-time case code (`7KQ-42M-RX`). The reporter chooses the passphrase. |
| `CheckReport` | anyone with the code and passphrase | The status and the thread. |
| `ReplyToReport` | anyone with the code and passphrase | Adds the reporter's message; a case waiting on the reporter goes back to "In review". |
| `SubmitNamedReport` | a signed-in user | Files a report as that user. |
| `ListMyReports`, `GetMyReport`, `ReplyToMyReport` | a signed-in user | Only that user's own named reports. |

The reporter's view (`ReporterView`) is the status, the details and the thread. It never carries
internal notes, the assessment, notices, the assignee or which officer wrote a message.

### Anonymity

- No request asks who the reporter is, and the anonymous RPCs never read a forwarded actor: the
  caller policy lets the gateway call them only as itself, so a signed-in user who chooses
  anonymous stays anonymous. Identity is never asked about an anonymous reporter.
- The IP address never reaches this service: anonymous reports arrive through the separate
  anonymous front end, which strips it, and the service logs no request peer.
- The passphrase is stored only as an argon2id hash (19 MiB, two passes). An unknown case code and
  a wrong passphrase get the same `REPORT_NOT_FOUND` answer and cost the same work. Throttle
  repeated tries at the gateway; every refused try is audited as `report.check_refused`.
- A case code is 8 characters from an alphabet without 0, 1, I, L, O or U, shown in groups of
  three. Input is matched ignoring case, spaces and dashes.
- Attachments: up to three, 10 MB each. The type is sniffed from the bytes. JPEG, PNG and GIF are
  decoded and encoded again, so no EXIF, XMP, comment or text chunk survives; plain UTF-8 text is
  kept as is; anything else is refused with `ATTACHMENT_UNSUPPORTED`. Every attachment is stored
  as `attachment-<n>.<ext>`: an uploaded name can carry a person's name.
- A schema check refuses a reporter id on an anonymous case.

## CaseService: the officer's side

Every call needs a signed-in user who is in one of `REPORTING_OFFICER_GROUPS`, checked against
identity on every call through steward-authz's rule engine. Site admins, root and compliance
admins are not officers unless they are in one of those groups, and act-as is refused
(`ACT_AS_NOT_ALLOWED`). Every refusal of a known user is audited as `case.access_refused`.

| RPC | What |
| --- | --- |
| `ListCases` | The queue, newest first, filtered by status and assignee, with the count per status over every case and each case's next deadline. |
| `GetCase` | The report, attachments, thread, internal notes, latest assessment, notices and close-out. |
| `GetAttachment` | A stored attachment's bytes. |
| `PostMessage` | Writes to the reporter's thread; the case moves to "Needs reporter reply". |
| `AddNote` | An internal note. |
| `AssignCase` | Sets the assignee, who must be an officer, or clears it. |
| `SetCaseStatus` | Moves an open case to any status but closed. |
| `SetDiscoveryDate` | When the incident was discovered (not in the future); every deadline moves with it. |
| `RecordRiskAssessment` | Four factors (information involved, who received it, whether it was viewed, how far the risk was reduced), a suggested result, and the officer's decision with a required reason. Reportable moves the case to "Notification due", not reportable back to "In review". The latest assessment is the case's. |
| `AddNotice`, `UpdateNotice` | A notice to affected people, a regulator, the media or someone else, due the discovery date plus the configured days; its status and the date it was sent. |
| `CloseCase` | The outcome, corrective actions (each optionally carrying a core policy id, a UUID) and an optional closing message posted to the reporter's thread. A closed case takes no more changes or replies (`CASE_CLOSED`) but can still be read. |

The suggestion is low risk when the risk was fully reduced, when the information was never viewed,
or when only staff saw information that is neither health nor financial information; anything else,
"not sure" included, suggests notification. The officer decides.

## Audit events

Every view and change publishes a `steward.audit.v1.AuditEvent` (tier `audit`) through the
go-outbox table `audit_outbox`, in the same transaction as the change.

| Action | Actor | Subject |
| --- | --- | --- |
| `report.submitted`, `report.checked`, `report.replied`, `report.viewed` | none | `case:<id>` |
| `report.check_refused`, `report.listed` | none | none |
| `case.listed` | the officer | none |
| `case.viewed`, `case.attachment_viewed`, `case.message_posted`, `case.note_added`, `case.assigned`, `case.status_changed`, `case.discovery_date_set`, `case.assessment_recorded`, `case.notice_added`, `case.notice_updated`, `case.closed` | the officer | `case:<id>` |
| `case.access_refused` | the refused user (the real user during act-as) | `case:<id>` when the call named one |
| `reporting.call.refused` | none | `method:<full method>` |

Audit readers are not the officer group, so no event carries report text, notes, messages,
reasons or the case code, and reporter-side events name no actor, a named reporter included.
Attributes carry only kinds, counts, statuses, decisions, outcomes and ids.

## Errors

Coded errors carry an `ErrorInfo` (domain `reporting`); see [error codes](error-codes.md).

## Calling other services

- **steward-identity** `IdentityReadService.GetUser`, to decide officer membership. Reporting asks
  as itself, with its service-account token and no end-user actor. Pinned in `proto-refs.env`
  (`STEWARD_IDENTITY_REF`), stubs in `gen/go/thirdparty/identity/v1`.
- **steward-audit**: publishes its `AuditEvent` (`STEWARD_AUDIT_REF`), never calls it.
- **steward-core** is not called. A corrective action stores core's policy id as given.
  `STEWARD_CORE_REF` pins only the `internal/workloadauth` copy, which CI compares byte for byte with
  core's (`scripts/workloadauth-check.sh`).

## Service-to-service authentication

Every call but `grpc.health.v1` needs the caller's projected service-account token (audience
`steward`) as `authorization: Bearer`. The only caller is `steward-gateway`: as itself on
`SubmitAnonymousReport`, `CheckReport` and `ReplyToReport`, and on behalf of the signed-in user on
every other RPC. Any other service account is refused with `PermissionDenied` and audited.

## Health

`grpc.health.v1/Check` answers carry `steward-version`, `steward-commit`, `steward-dep-postgres`
and `steward-depstate-<name>`; see the [runbook](runbook.md#probes).
