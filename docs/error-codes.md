# Error codes

Every coded gRPC error from the reporting service carries an `ErrorInfo` with the symbol as
its reason, the domain `reporting` and the code in `codeNum`. Only user-safe messages reach
the caller; every other code is sent as `Code N: Internal Error`.

| Code | Symbol | Area | Cause | User-safe |
| --- | --- | --- | --- | --- |
| 8100 | `INTERNAL` | reporting | an uncoded failure inside the reporting service | no |
| 8101 | `REPORT_STORE_UNAVAILABLE` | reporting store | a reporting Postgres read or write failed; the op metadata names it, the cause is only logged | no |
| 8102 | `REPORT_INVALID` | report or case input | a request field is missing, too long or out of range; field names it | yes |
| 8103 | `REPORT_NOT_FOUND` | check a report | no anonymous report matches the case code and passphrase; an unknown code and a wrong passphrase are not told apart | yes |
| 8104 | `REPORT_SIGN_IN_REQUIRED` | named reports and cases | a named-report or case call ran without a trusted forwarded actor | yes |
| 8105 | `CASE_ACCESS_DENIED` | cases | the actor is not in an officer group; site admins and root are not officers unless they are in one | yes |
| 8106 | `CASE_NOT_FOUND` | cases | no case or no attachment, notice or own named report with that id | yes |
| 8107 | `CASE_CLOSED` | cases | a change or a reply was sent to a closed case | yes |
| 8108 | `ATTACHMENT_UNSUPPORTED` | attachments | an attachment is not a JPEG, PNG, GIF or plain text file, can't be decoded, or is too large; attachment is its 1-based number | yes |
| 8109 | `IDENTITY_UNAVAILABLE` | officer check | identity could not be asked whether the actor is an officer, so the call is refused | no |
| 8110 | `ACT_AS_NOT_ALLOWED` | act-as | an impersonated actor called a case or named-report RPC; act-as never reaches cases or reports | yes |
| 8111 | `SETTINGS_ACCESS_DENIED` | compliance settings | the actor holds neither compliance.manage nor root | yes |
| 8112 | `PUBLIC_LINK_OFF` | anonymous reports | the public report link is switched off in the Compliance settings, so new anonymous reports are refused | yes |
