// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package authz_test

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Bugs5382/go-authz/rbac"

	"github.com/Steward-GRC/steward-reporting/internal/authz"
)

// The embedded catalog is a copy of the canonical Steward catalog. It must
// parse to the same catalog and be byte-identical, so a refresh is one command
// (task catalog:refresh) and a hand edit fails here.
func TestEmbeddedCatalogMatchesCanonical(t *testing.T) {
	canonical, err := os.ReadFile("../../testdata/catalog.canonical.json")
	if err != nil {
		t.Fatal(err)
	}
	want, err := rbac.Parse(canonical)
	if err != nil {
		t.Fatalf("canonical catalog does not parse: %v", err)
	}
	if diff := rbac.Diff(authz.Catalog, want); len(diff) != 0 {
		t.Fatalf("embedded catalog differs from the canonical one:\n%s", strings.Join(diff, "\n"))
	}
	embedded, err := os.ReadFile("catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embedded, canonical) {
		t.Fatal("internal/authz/catalog.json is not byte-identical to testdata/catalog.canonical.json")
	}
}

func TestCatalogSourceNamesTheCommit(t *testing.T) {
	b, err := os.ReadFile("../../testdata/CATALOG_SOURCE")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		k, v, _ := strings.Cut(line, ": ")
		got[k] = v
	}
	if len(got["commit"]) != 40 || got["path"] != "internal/authz/catalog.json" || got["repo"] == "" {
		t.Fatalf("CATALOG_SOURCE needs commit, path and repo, got %v", got)
	}
}

func TestRolePermissions(t *testing.T) {
	if got := authz.RolePermissions(authz.RoleComplianceAdmin); !slices.Contains(got, authz.ComplianceManage) {
		t.Errorf("compliance-admin must hold compliance.manage, got %v", got)
	}
	if got := authz.RolePermissions(authz.RoleSiteAdmin); !slices.Contains(got, authz.ComplianceManage) {
		t.Errorf("site-admin must hold compliance.manage through the wildcard, got %v", got)
	}
	for _, r := range []authz.Role{authz.RoleReader, authz.RoleAuthor, authz.RoleApprover, authz.RoleTemplateAdmin} {
		if slices.Contains(authz.RolePermissions(r), authz.ComplianceManage) {
			t.Errorf("role %q must not hold compliance.manage", r)
		}
	}
}

func TestParseRole(t *testing.T) {
	if r, err := authz.ParseRole("compliance-admin"); err != nil || r != authz.RoleComplianceAdmin {
		t.Fatalf("ParseRole(compliance-admin) = %q, %v", r, err)
	}
	if _, err := authz.ParseRole("admin"); err == nil {
		t.Fatal("an unknown role must not parse")
	}
}

// Every constant must exist in the catalog and every catalog permission and
// role must have a constant, so the two cannot drift apart.
func TestConstantsMatchTheCatalog(t *testing.T) {
	perms := []authz.Permission{
		authz.PolicyRead, authz.PolicyReadSensitive, authz.PolicyAuthor, authz.PolicySubmit, authz.PolicyApprove,
		authz.TemplateManage, authz.WorkflowManage, authz.GroupManage, authz.UserManage, authz.RoleManage,
		authz.AuditRead, authz.ComplianceManage, authz.ComplianceReport, authz.SettingsManage,
		authz.DeliveryManage, authz.SessionManage,
	}
	for _, p := range perms {
		if _, err := authz.Catalog.ParsePermission(string(p)); err != nil {
			t.Errorf("constant %q is not in the catalog: %v", p, err)
		}
	}
	for _, p := range authz.Catalog.PermissionNames() {
		if !slices.Contains(perms, p) {
			t.Errorf("catalog permission %q has no constant", p)
		}
	}
	roles := []authz.Role{
		authz.RoleReader, authz.RoleAuthor, authz.RoleApprover,
		authz.RoleTemplateAdmin, authz.RoleComplianceAdmin, authz.RoleSiteAdmin,
	}
	for _, r := range roles {
		if _, err := authz.Catalog.ParseRole(string(r)); err != nil {
			t.Errorf("constant %q is not in the catalog: %v", r, err)
		}
	}
	for _, r := range authz.Catalog.RoleNames() {
		if !slices.Contains(roles, r) {
			t.Errorf("catalog role %q has no constant", r)
		}
	}
}
