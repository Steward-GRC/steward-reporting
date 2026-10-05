// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/grpcsvc"
	"github.com/Steward-GRC/steward-reporting/internal/workloadauth"
)

func methodsOf(sd protoreflect.ServiceDescriptor) []string {
	var out []string
	for i := range sd.Methods().Len() {
		out = append(out, "/"+string(sd.FullName())+"/"+string(sd.Methods().Get(i).Name()))
	}
	return out
}

var anonymousMethods = map[string]bool{
	reportingv1.IntakeService_SubmitAnonymousReport_FullMethodName: true,
	reportingv1.IntakeService_CheckReport_FullMethodName:           true,
	reportingv1.IntakeService_ReplyToReport_FullMethodName:         true,
}

// The gateway is the only caller. On the anonymous methods it acts only as
// itself, so no end-user actor can ride along; on the rest it acts for the
// signed-in user. Every other service account is refused everywhere.
func TestCallerPolicyPerMethod(t *testing.T) {
	all := append(methodsOf(reportingv1.File_steward_reporting_v1_intake_proto.Services().Get(0)),
		methodsOf(reportingv1.File_steward_reporting_v1_cases_proto.Services().Get(0))...)
	require.Len(t, all, 19)
	for _, method := range all {
		t.Run(method, func(t *testing.T) {
			acc, ok := grpcsvc.CallerPolicy.Lookup(method, grpcsvc.CallerGateway)
			require.True(t, ok, "the gateway calls %s", method)
			want := workloadauth.OnBehalf
			if anonymousMethods[method] {
				want = workloadauth.Self
			}
			require.Equal(t, want, acc)
			require.Len(t, grpcsvc.CallerPolicy[method], 1, "only the gateway is listed")
			for _, other := range []string{"core", "identity", "obligations", "audit", "steward-gateway"} {
				_, ok := grpcsvc.CallerPolicy.Lookup(method, other)
				require.False(t, ok, "%s may not call %s", other, method)
			}
		})
	}
	require.Len(t, grpcsvc.CallerPolicy, len(all), "every served method is listed, and nothing else")
}
