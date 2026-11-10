// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package grpcsvc

import (
	workloadidentity "github.com/Bugs5382/go-workload-identity"
	reportingv1 "github.com/Steward-GRC/steward-reporting/gen/go/steward/reporting/v1"
)

// CallerGateway is the gateway's caller name: its service account
// steward-gateway without the steward- prefix.
const CallerGateway = "gateway"

// CallerPolicy is reporting's per-method allow-list. The gateway is the only
// caller. On the anonymous methods it acts only as itself, so no end-user
// actor can reach them; on every other method it acts for the signed-in
// user.
var CallerPolicy = workloadidentity.Policy{
	reportingv1.IntakeService_GetIntakeOptions_FullMethodName:      {CallerGateway: workloadidentity.Self},
	reportingv1.IntakeService_SubmitAnonymousReport_FullMethodName: {CallerGateway: workloadidentity.Self},
	reportingv1.IntakeService_CheckReport_FullMethodName:           {CallerGateway: workloadidentity.Self},
	reportingv1.IntakeService_ReplyToReport_FullMethodName:         {CallerGateway: workloadidentity.Self},
	reportingv1.IntakeService_SubmitNamedReport_FullMethodName:     {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.IntakeService_ListMyReports_FullMethodName:         {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.IntakeService_GetMyReport_FullMethodName:           {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.IntakeService_ReplyToMyReport_FullMethodName:       {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_ListCases_FullMethodName:               {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_GetCase_FullMethodName:                 {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_GetAttachment_FullMethodName:           {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_PostMessage_FullMethodName:             {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_AddNote_FullMethodName:                 {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_AssignCase_FullMethodName:              {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_SetCaseStatus_FullMethodName:           {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_SetDiscoveryDate_FullMethodName:        {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_RecordRiskAssessment_FullMethodName:    {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_AddNotice_FullMethodName:               {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_UpdateNotice_FullMethodName:            {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.CaseService_CloseCase_FullMethodName:               {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.SettingsService_GetSettings_FullMethodName:         {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.SettingsService_UpdateSettings_FullMethodName:      {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.LegalHoldService_PlaceLegalHold_FullMethodName:     {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.LegalHoldService_ReleaseLegalHold_FullMethodName:   {CallerGateway: workloadidentity.OnBehalf},
	reportingv1.LegalHoldService_ListLegalHolds_FullMethodName:     {CallerGateway: workloadidentity.OnBehalf},
}
