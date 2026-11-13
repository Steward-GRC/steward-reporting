-- Copyright 2026 The Steward Authors
-- SPDX-License-Identifier: Apache-2.0

-- The Compliance module off switch (parity row 71). Off, every intake and case
-- call is refused and the data is kept. It has its own column and its own
-- RPC: UpdateSettings replaces the other settings and never touches it, so an
-- older client that omits the field cannot switch the module off.
ALTER TABLE compliance_settings ADD COLUMN module_enabled boolean NOT NULL DEFAULT true;
