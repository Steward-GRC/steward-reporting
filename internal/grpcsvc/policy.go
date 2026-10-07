// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc

import (
	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
	"github.com/Steward-GRC/steward-reporting/internal/workloadauth"
)

// CallerGateway is the gateway's caller name: its service account
// steward-gateway without the steward- prefix.
const CallerGateway = "gateway"

// CallerPolicy is reporting's per-method allow-list. The gateway is the only
// caller. On the anonymous methods it acts only as itself, so no end-user
// actor can reach them; on every other method it acts for the signed-in
// user.
var CallerPolicy = workloadauth.Policy{
	reportingv1.IntakeService_GetIntakeOptions_FullMethodName:      {CallerGateway: workloadauth.Self},
	reportingv1.IntakeService_SubmitAnonymousReport_FullMethodName: {CallerGateway: workloadauth.Self},
	reportingv1.IntakeService_CheckReport_FullMethodName:           {CallerGateway: workloadauth.Self},
	reportingv1.IntakeService_ReplyToReport_FullMethodName:         {CallerGateway: workloadauth.Self},
	reportingv1.IntakeService_SubmitNamedReport_FullMethodName:     {CallerGateway: workloadauth.OnBehalf},
	reportingv1.IntakeService_ListMyReports_FullMethodName:         {CallerGateway: workloadauth.OnBehalf},
	reportingv1.IntakeService_GetMyReport_FullMethodName:           {CallerGateway: workloadauth.OnBehalf},
	reportingv1.IntakeService_ReplyToMyReport_FullMethodName:       {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_ListCases_FullMethodName:               {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_GetCase_FullMethodName:                 {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_GetAttachment_FullMethodName:           {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_PostMessage_FullMethodName:             {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_AddNote_FullMethodName:                 {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_AssignCase_FullMethodName:              {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_SetCaseStatus_FullMethodName:           {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_SetDiscoveryDate_FullMethodName:        {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_RecordRiskAssessment_FullMethodName:    {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_AddNotice_FullMethodName:               {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_UpdateNotice_FullMethodName:            {CallerGateway: workloadauth.OnBehalf},
	reportingv1.CaseService_CloseCase_FullMethodName:               {CallerGateway: workloadauth.OnBehalf},
	reportingv1.SettingsService_GetSettings_FullMethodName:         {CallerGateway: workloadauth.OnBehalf},
	reportingv1.SettingsService_UpdateSettings_FullMethodName:      {CallerGateway: workloadauth.OnBehalf},
}
