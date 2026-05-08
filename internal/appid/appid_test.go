package appid

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulmenhq/gofulmen/appidentity"

	appidentityassets "github.com/fulmenhq/limensafe/internal/assets/appidentity"
)

func prepareIdentityForTest(t *testing.T) {
	t.Helper()

	// Ensure per-test isolation.
	//
	// gofulmen caches identity per-process, and embedded identity registration is
	// also stored globally. Reset clears both.
	appidentity.Reset()

	// Re-register embedded identity so standalone behavior is always available
	// in tests.
	if err := appidentity.RegisterEmbeddedIdentityYAML(appidentityassets.YAML); err != nil {
		t.Fatalf("RegisterEmbeddedIdentityYAML: %v", err)
	}

	t.Cleanup(func() { appidentity.Reset() })
}

func TestGet_EmbeddedIdentityFallbackOutsideRepo(t *testing.T) {
	prepareIdentityForTest(t)
	t.Setenv(appidentity.EnvIdentityPath, "")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	outside := t.TempDir()
	if err := os.Chdir(outside); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	identity, err := Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if identity.BinaryName == "" {
		t.Fatalf("expected BinaryName to be set")
	}
	if identity.EnvPrefix == "" {
		t.Fatalf("expected EnvPrefix to be set")
	}
}

// TestGet_EmbeddedIdentityWinsOverForeignCWD reproduces the partner-integration (devlead,
// 2026-05-08) symptom: when the limensafe binary is run from inside another
// repo's working tree (e.g., datawidget's), gofulmen's discovery walks up
// from CWD and finds the foreign `.fulmen/app.yaml` before falling back to the
// embedded identity — so the binary mis-identifies itself as the foreign app.
//
// Expected behaviour: a binary that has registered an embedded identity (via
// init() / RegisterEmbeddedIdentityYAML) IS that app. Self-identification must
// not be shadowed by an unrelated repo's identity file just because that's
// where the user happens to be standing.
//
// This test is currently EXPECTED TO FAIL against gofulmen v0.3.3 — it
// documents the bug. Fix lands either in gofulmen (precedence reorder so
// embedded > CWD ancestor search) or as a local workaround in this package.
func TestGet_EmbeddedIdentityWinsOverForeignCWD(t *testing.T) {
	prepareIdentityForTest(t)
	t.Setenv(appidentity.EnvIdentityPath, "")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Build a foreign repo tree with its own .fulmen/app.yaml claiming a
	// different binary identity (mimics running limensafe from inside the
	// datawidget repo).
	foreignRoot := t.TempDir()
	foreignFulmen := filepath.Join(foreignRoot, ".fulmen")
	if err := os.MkdirAll(foreignFulmen, 0o755); err != nil {
		t.Fatalf("mkdir foreign .fulmen: %v", err)
	}
	foreignYAML := []byte(`# Foreign app identity (would shadow limensafe's embedded copy)
app:
  vendor: example
  binary_name: foreign-app
  env_prefix: FOREIGN_
  config_name: foreign-app
  description: Synthetic foreign repo for the identity-shadow regression test
  version: 0.0.1
metadata:
  repository_category: cli
  project_url: https://example.invalid/foreign-app
  license: Apache-2.0
`)
	if err := os.WriteFile(filepath.Join(foreignFulmen, "app.yaml"), foreignYAML, 0o644); err != nil {
		t.Fatalf("write foreign app.yaml: %v", err)
	}

	if err := os.Chdir(foreignRoot); err != nil {
		t.Fatalf("chdir foreign root: %v", err)
	}

	identity, err := Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if identity.BinaryName != "limensafe" {
		t.Fatalf("identity-shadow bug reproduced: expected BinaryName=limensafe (embedded), got %q (foreign CWD shadowed)", identity.BinaryName)
	}
	if identity.EnvPrefix != "LIMENSAFE_" {
		t.Fatalf("expected EnvPrefix=LIMENSAFE_, got %q", identity.EnvPrefix)
	}
}

func TestGet_EnvVarRemainsAuthoritative(t *testing.T) {
	prepareIdentityForTest(t)

	missing := filepath.Join(t.TempDir(), "missing-app.yaml")
	t.Setenv(appidentity.EnvIdentityPath, missing)

	_, err := Get(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}

	var notFound *appidentity.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected NotFoundError, got %T: %v", err, err)
	}
}
