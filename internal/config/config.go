// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package config reads the reporting service's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Steward-GRC/steward-reporting/internal/domain"
	"github.com/Steward-GRC/steward-reporting/internal/workloadauth"
)

// DefaultTokenFile is where the release mounts reporting's projected
// service-account token.
const DefaultTokenFile = workloadauth.DefaultTokenFile

// defaultNoticeDays is the days allowed from discovery when an adopter sets
// none.
const defaultNoticeDays = 60

// Config is every setting the service runs with.
type Config struct {
	DatabaseDSN string
	// MigrateDSN is a direct connection for migrations; defaults to
	// DatabaseDSN.
	MigrateDSN    string
	MigrationsDir string
	RabbitURL     string
	GRPCPort      string
	// ProbePort serves /livez and /readyz over plain HTTP.
	ProbePort    string
	OTLPEndpoint string
	IdentityAddr string
	// OfficerGroups are the local group ids or identity provider group names
	// whose members work cases. Empty means nobody does.
	OfficerGroups []string
	// NoticeDays are the days allowed from discovery per notice recipient.
	NoticeDays domain.NoticeDays
	// WorkloadAuth is false only for WORKLOAD_AUTH=disabled, on local runs.
	WorkloadAuth bool
	// Workload verifies the callers' service-account tokens.
	Workload workloadauth.Config
	// TokenFile is reporting's own projected token, sent on every call to
	// identity; empty when WorkloadAuth is off.
	TokenFile string
}

// Load reads the settings through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	or := func(k, d string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return d
	}
	c := Config{
		DatabaseDSN:   getenv("DATABASE_DSN"),
		MigrationsDir: or("MIGRATIONS_DIR", "migrations"),
		RabbitURL:     getenv("RABBITMQ_URL"),
		GRPCPort:      or("GRPC_PORT", "9090"),
		ProbePort:     or("PROBE_PORT", "8080"),
		OTLPEndpoint:  or("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		IdentityAddr:  or("IDENTITY_GRPC_ADDR", "identity:9090"),
		OfficerGroups: list(getenv("REPORTING_OFFICER_GROUPS")),
	}
	c.MigrateDSN = or("MIGRATE_DSN", c.DatabaseDSN)

	var errs []error
	if c.DatabaseDSN == "" {
		errs = append(errs, errors.New("DATABASE_DSN is required"))
	}
	if c.RabbitURL == "" {
		errs = append(errs, errors.New("RABBITMQ_URL is required"))
	}
	days := func(k string) int {
		raw := or(k, strconv.Itoa(defaultNoticeDays))
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s must be a whole number of days above 0, got %q", k, raw))
		}
		return n
	}
	c.NoticeDays = domain.NoticeDays{
		Affected:  days("REPORTING_NOTICE_DAYS_AFFECTED"),
		Regulator: days("REPORTING_NOTICE_DAYS_REGULATOR"),
		Media:     days("REPORTING_NOTICE_DAYS_MEDIA"),
		Other:     days("REPORTING_NOTICE_DAYS_OTHER"),
	}
	var err error
	if c.Workload, c.WorkloadAuth, err = workloadauth.ServerConfigFromEnv(getenv); err != nil {
		errs = append(errs, err)
	}
	if c.WorkloadAuth {
		c.TokenFile = or(workloadauth.EnvTokenFile, DefaultTokenFile)
	}
	return c, errors.Join(errs...)
}

// list splits a comma-separated value, dropping blanks, so a stray comma
// never adds an empty entry.
func list(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
