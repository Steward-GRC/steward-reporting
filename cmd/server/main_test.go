// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	log "github.com/Bugs5382/go-log"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/Steward-GRC/steward-reporting/internal/audit"
	"github.com/Steward-GRC/steward-reporting/internal/workloadauth"
)

type recordingPublisher struct{ bodies [][]byte }

func (r *recordingPublisher) Publish(_ context.Context, _ string, body []byte) error {
	r.bodies = append(r.bodies, body)
	return nil
}

// A refused call is audited with who called what and why, never the request.
func TestAuditDenialRecordsTheRefusal(t *testing.T) {
	pub := &recordingPublisher{}
	auditDenial(audit.New(pub), log.Nop())(t.Context(), workloadauth.Denial{
		Method: "/steward.reporting.v1.CaseService/GetCase",
		Caller: workloadauth.Caller{Name: "workflow", ServiceAccount: "steward/steward-workflow"},
		Code:   codes.PermissionDenied, Reason: workloadauth.ReasonMethodNotAllowed,
		Request: "secret request body",
	})
	require.Len(t, pub.bodies, 1)
	ev, err := audit.Decode(pub.bodies[0])
	require.NoError(t, err)
	require.Equal(t, "reporting.call.refused", ev.Action)
	require.Equal(t, "method:/steward.reporting.v1.CaseService/GetCase", ev.Subject)
	require.Equal(t, "workflow", ev.Attributes["caller"])
	require.Equal(t, "steward/steward-workflow", ev.Attributes["service_account"])
	require.Equal(t, "PermissionDenied", ev.Attributes["code"])
	for _, v := range ev.Attributes {
		require.NotContains(t, v, "secret request body")
	}
}
