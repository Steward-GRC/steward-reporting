-- Copyright 2026 The Steward Authors
-- SPDX-License-Identifier: Apache-2.0

-- A case is one report and the officer work on it. An anonymous case keeps
-- no reporter id: only the case code and the passphrase hash lead back to it.
CREATE TABLE cases (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    case_code         text        NOT NULL UNIQUE,
    kind              text        NOT NULL CHECK (kind IN ('anonymous', 'named')),
    reporter_user_id  text,
    passphrase_hash   text,
    status            text        NOT NULL DEFAULT 'new' CHECK (status IN (
                          'new', 'in_review', 'needs_reporter_reply', 'risk_assessment', 'notification_due', 'closed')),
    what_happened     text        NOT NULL,
    occurred          text        NOT NULL DEFAULT '',
    location          text        NOT NULL DEFAULT '',
    information_kinds text[]      NOT NULL DEFAULT '{}',
    still_happening   text        NOT NULL DEFAULT '' CHECK (still_happening IN ('', 'yes', 'no', 'not_sure')),
    category          text        NOT NULL DEFAULT '',
    assignee_user_id  text,
    received_at       timestamptz NOT NULL DEFAULT now(),
    discovered_on     date        NOT NULL,
    outcome           text        CHECK (outcome IN ('substantiated', 'not_substantiated', 'inconclusive')),
    closed_at         timestamptz,
    CONSTRAINT cases_reporter_matches_kind CHECK (
        (kind = 'anonymous' AND reporter_user_id IS NULL AND passphrase_hash IS NOT NULL)
        OR (kind = 'named' AND reporter_user_id IS NOT NULL AND passphrase_hash IS NULL)),
    CONSTRAINT cases_closed_has_outcome CHECK (
        (status = 'closed') = (closed_at IS NOT NULL AND outcome IS NOT NULL))
);

CREATE INDEX cases_queue_idx ON cases (status, received_at DESC);
CREATE INDEX cases_reporter_idx ON cases (reporter_user_id) WHERE reporter_user_id IS NOT NULL;

-- Attachments are stored after their metadata is stripped.
CREATE TABLE case_attachments (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id      uuid        NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    position     int         NOT NULL,
    filename     text        NOT NULL,
    content_type text        NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'image/gif', 'text/plain')),
    size_bytes   bigint      NOT NULL,
    data         bytea       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (case_id, position)
);

-- The two-way thread with the reporter. An officer message names its
-- officer; a reporter message names nobody.
CREATE TABLE case_messages (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id         uuid        NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    author          text        NOT NULL CHECK (author IN ('reporter', 'officer')),
    officer_user_id text,
    body            text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT case_messages_officer_named CHECK ((author = 'officer') = (officer_user_id IS NOT NULL))
);

CREATE INDEX case_messages_case_idx ON case_messages (case_id, created_at);

-- Internal notes, never shown to the reporter.
CREATE TABLE case_notes (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id        uuid        NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    author_user_id text        NOT NULL,
    body           text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX case_notes_case_idx ON case_notes (case_id, created_at);

-- Every recorded risk assessment; the latest is the case's current one.
CREATE TABLE case_assessments (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id            uuid        NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    information_kinds  text[]      NOT NULL,
    recipient          text        NOT NULL CHECK (recipient IN ('staff_only', 'unknown_people', 'another_organisation')),
    viewed             text        NOT NULL CHECK (viewed IN ('yes', 'probably', 'no')),
    mitigation         text        NOT NULL CHECK (mitigation IN ('fully', 'partly', 'not_at_all')),
    suggestion         text        NOT NULL CHECK (suggestion IN ('notification_likely_required', 'low_probability_of_compromise')),
    decision           text        NOT NULL CHECK (decision IN ('reportable', 'not_reportable')),
    reason             text        NOT NULL,
    decided_by_user_id text        NOT NULL,
    decided_at         timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX case_assessments_case_idx ON case_assessments (case_id, decided_at DESC);

-- The notices a case owes. The deadline is the case's discovery date plus
-- days_allowed, so it moves when the discovery date does.
CREATE TABLE case_notices (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id      uuid        NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    recipient    text        NOT NULL CHECK (recipient IN ('affected_people', 'regulator', 'media', 'other')),
    label        text        NOT NULL DEFAULT '',
    method       text        NOT NULL DEFAULT '',
    days_allowed int         NOT NULL CHECK (days_allowed > 0),
    status       text        NOT NULL DEFAULT 'not_sent' CHECK (status IN ('not_sent', 'draft', 'sent', 'not_needed')),
    sent_on      date,
    created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT case_notices_sent_dated CHECK ((status = 'sent') = (sent_on IS NOT NULL))
);

CREATE INDEX case_notices_case_idx ON case_notices (case_id, created_at);

-- Corrective actions recorded at close-out; policy_id is core's policy id.
CREATE TABLE case_corrective_actions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id     uuid NOT NULL REFERENCES cases (id) ON DELETE CASCADE,
    position    int  NOT NULL,
    description text NOT NULL,
    policy_id   uuid,
    UNIQUE (case_id, position)
);

-- The Compliance settings (C10): one row, seeded at start-up from
-- REPORTING_OFFICER_GROUPS and changed only through SettingsService after.
CREATE TABLE compliance_settings (
    id                  boolean     PRIMARY KEY DEFAULT true CHECK (id),
    officer_groups      text[]      NOT NULL DEFAULT '{}',
    public_link_enabled boolean     NOT NULL DEFAULT true,
    retention_days      int         NOT NULL CHECK (retention_days BETWEEN 30 AND 36500),
    intake_categories   jsonb       NOT NULL DEFAULT '[]',
    updated_at          timestamptz NOT NULL DEFAULT now(),
    updated_by_user_id  text
);

-- A case under a legal hold is never purged. RESTRICT makes the database
-- refuse to delete a held case, whatever deletes it.
CREATE TABLE case_legal_holds (
    case_id           uuid        PRIMARY KEY REFERENCES cases (id) ON DELETE RESTRICT,
    placed_by_user_id text        NOT NULL,
    placed_at         timestamptz NOT NULL DEFAULT now()
);

-- The retention purge looks for closed cases by close date.
CREATE INDEX cases_closed_idx ON cases (closed_at) WHERE status = 'closed';
