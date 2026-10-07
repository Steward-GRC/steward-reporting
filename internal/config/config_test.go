// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func required() map[string]string {
	return withWorkload(base())
}

func base() map[string]string {
	return map[string]string{
		"DATABASE_DSN": "postgres://reporting:reporting@localhost:5432/reporting",
		"RABBITMQ_URL": "amqp://guest:guest@localhost:5672/",
	}
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(required()))
	require.NoError(t, err)
	require.Equal(t, "9090", c.GRPCPort)
	require.Equal(t, "8080", c.ProbePort)
	require.Equal(t, "migrations", c.MigrationsDir)
	require.Equal(t, c.DatabaseDSN, c.MigrateDSN)
	require.Equal(t, "identity:9090", c.IdentityAddr)
	require.Empty(t, c.OfficerGroups, "nobody is an officer until an adopter names the groups")
	require.Equal(t, domain.NoticeDays{Affected: 60, Regulator: 60, Media: 60, Other: 60}, c.NoticeDays)
}

func TestLoadMissingRequired(t *testing.T) {
	for _, k := range []string{"DATABASE_DSN", "RABBITMQ_URL"} {
		m := required()
		delete(m, k)
		_, err := Load(env(m))
		require.ErrorContains(t, err, k)
	}
}

func TestLoadOfficerGroups(t *testing.T) {
	m := required()
	m["REPORTING_OFFICER_GROUPS"] = " Privacy Officers ,, 9c1d4e2a-0000-4000-8000-000000000001,"
	c, err := Load(env(m))
	require.NoError(t, err)
	require.Equal(t, []string{"Privacy Officers", "9c1d4e2a-0000-4000-8000-000000000001"}, c.OfficerGroups)
}

func TestLoadNoticeDays(t *testing.T) {
	m := required()
	m["REPORTING_NOTICE_DAYS_REGULATOR"] = "30"
	m["REPORTING_NOTICE_DAYS_MEDIA"] = "45"
	c, err := Load(env(m))
	require.NoError(t, err)
	require.Equal(t, domain.NoticeDays{Affected: 60, Regulator: 30, Media: 45, Other: 60}, c.NoticeDays)

	for _, bad := range []string{"soon", "0", "-5"} {
		m["REPORTING_NOTICE_DAYS_AFFECTED"] = bad
		_, err = Load(env(m))
		require.ErrorContains(t, err, "REPORTING_NOTICE_DAYS_AFFECTED", bad)
	}
}

const issuer = "https://issuer.example.org"

func withWorkload(m map[string]string) map[string]string {
	m["WORKLOAD_OIDC_ISSUER"] = issuer
	m["WORKLOAD_ALLOWED_SERVICEACCOUNTS"] = "steward/steward-gateway"
	return m
}

func TestLoadWorkloadAuth(t *testing.T) {
	c, err := Load(env(required()))
	require.NoError(t, err)
	require.True(t, c.WorkloadAuth)
	require.Equal(t, issuer, c.Workload.Issuer)
	require.Equal(t, "steward", c.Workload.Audience)
	require.Equal(t, []string{"steward/steward-gateway"}, c.Workload.AllowedServiceAccounts)
	require.Equal(t, DefaultTokenFile, c.TokenFile)
}

// Authentication fails closed: no issuer is a start-up error unless it is
// switched off on purpose.
func TestLoadWorkloadAuthFailsClosed(t *testing.T) {
	_, err := Load(env(base()))
	require.ErrorContains(t, err, "WORKLOAD_OIDC_ISSUER")

	m := base()
	m["WORKLOAD_AUTH"] = "off"
	_, err = Load(env(m))
	require.ErrorContains(t, err, "WORKLOAD_AUTH")
}

func TestLoadWorkloadAuthDisabled(t *testing.T) {
	m := base()
	m["WORKLOAD_AUTH"] = "disabled"
	c, err := Load(env(m))
	require.NoError(t, err)
	require.False(t, c.WorkloadAuth)
	require.Empty(t, c.TokenFile, "a disabled run sends no token")
}

func TestLoadTokenFile(t *testing.T) {
	m := required()
	m["WORKLOAD_TOKEN_FILE"] = "/run/token"
	c, err := Load(env(m))
	require.NoError(t, err)
	require.Equal(t, "/run/token", c.TokenFile)
}

func TestLoadPurgeInterval(t *testing.T) {
	c, err := Load(env(required()))
	require.NoError(t, err)
	require.Equal(t, time.Hour, c.PurgeInterval, "the purge runs hourly by default")

	m := required()
	m["REPORTING_PURGE_INTERVAL"] = "15m"
	c, err = Load(env(m))
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, c.PurgeInterval)

	m["REPORTING_PURGE_INTERVAL"] = "off"
	c, err = Load(env(m))
	require.NoError(t, err)
	require.Zero(t, c.PurgeInterval, "off pauses the purge")

	for _, bad := range []string{"soon", "0s", "-1h", "30s"} {
		m["REPORTING_PURGE_INTERVAL"] = bad
		_, err = Load(env(m))
		require.ErrorContains(t, err, "REPORTING_PURGE_INTERVAL", bad)
	}
}
