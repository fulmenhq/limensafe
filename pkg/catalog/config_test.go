package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigBytes_MinimalValid(t *testing.T) {
	yaml := `
schema_version: "1.0.0"
repo:
  id: cs-spike-repo-v0
  visibility: public_oss
catalogs:
  - catalog_id: cs-spike-public-v0
    source: { kind: file, path: ../catalog/x.yaml }
`
	cfg, err := LoadConfigBytes([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repo.ID != "cs-spike-repo-v0" {
		t.Errorf("repo.id: got %q", cfg.Repo.ID)
	}
	if len(cfg.Catalogs) != 1 {
		t.Fatalf("expected 1 catalog ref; got %d", len(cfg.Catalogs))
	}
}

func TestLoadConfigBytes_RequiresSchemaVersion(t *testing.T) {
	yaml := `
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: c
    source: {kind: file, path: x}
`
	_, err := LoadConfigBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("expected schema_version error, got: %v", err)
	}
}

func TestLoadConfigBytes_RequiresRepoIDAndVisibility(t *testing.T) {
	yaml := `
schema_version: "1.0.0"
repo: {visibility: public_oss}
catalogs:
  - catalog_id: c
    source: {kind: file, path: x}
`
	_, err := LoadConfigBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "repo.id") {
		t.Errorf("expected repo.id error, got: %v", err)
	}

	yaml = `
schema_version: "1.0.0"
repo: {id: r}
catalogs:
  - catalog_id: c
    source: {kind: file, path: x}
`
	_, err = LoadConfigBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "visibility") {
		t.Errorf("expected visibility error, got: %v", err)
	}
}

func TestLoadConfigBytes_RequiresCatalogs(t *testing.T) {
	yaml := `
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs: []
`
	_, err := LoadConfigBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "catalog") {
		t.Errorf("expected catalog error, got: %v", err)
	}
}

func TestLoadConfigBytes_FileSourceRequiresPath(t *testing.T) {
	yaml := `
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: c
    source: {kind: file}
`
	_, err := LoadConfigBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("expected path error, got: %v", err)
	}
}

func TestLoadConfigBytes_EnvSourceRequiresVar(t *testing.T) {
	yaml := `
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: c
    source: {kind: env}
`
	_, err := LoadConfigBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "var") {
		t.Errorf("expected var error, got: %v", err)
	}
}

func TestLoadConfigFile_SyntheticAcme(t *testing.T) {
	cfg, err := LoadConfigFile("../../testdata/synthetic-acme/.limensafe/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repo.ID != "cs-spike-repo-v0" {
		t.Errorf("repo.id: got %q", cfg.Repo.ID)
	}
	if cfg.Repo.Visibility != "public_oss" {
		t.Errorf("repo.visibility: got %q", cfg.Repo.Visibility)
	}
	if len(cfg.Catalogs) != 2 {
		t.Errorf("expected 2 catalog refs; got %d", len(cfg.Catalogs))
	}
}

func TestResolveCatalogs_FixtureWithEnvUnset(t *testing.T) {
	// Ensure env var is unset for this test to verify absent_optional.
	prev := os.Getenv("LIMENSAFE_CATALOG_PATH")
	_ = os.Unsetenv("LIMENSAFE_CATALOG_PATH")
	t.Cleanup(func() {
		if prev != "" {
			_ = os.Setenv("LIMENSAFE_CATALOG_PATH", prev)
		}
	})

	configPath, err := filepath.Abs("../../testdata/synthetic-acme/.limensafe/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	resolutions, err := cfg.ResolveCatalogs(configPath)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolutions) != 2 {
		t.Fatalf("expected 2 resolutions; got %d", len(resolutions))
	}

	// First catalog: file source, required → must load OK
	r0 := resolutions[0]
	if r0.CatalogID != "cs-spike-public-v0" || r0.LoadStatus != "ok" || r0.Catalog == nil {
		t.Errorf("public catalog: %+v", r0)
	}

	// Second catalog: env source, optional, env unset → absent_optional
	r1 := resolutions[1]
	if r1.CatalogID != "cs-spike-private-v0" || r1.LoadStatus != "absent_optional" || r1.Catalog != nil {
		t.Errorf("private catalog (env unset): %+v", r1)
	}
}

func TestResolveCatalogs_FixtureWithEnvSet(t *testing.T) {
	privateCatalog, err := filepath.Abs("../../testdata/synthetic-acme/catalog/synthetic-acme.catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIMENSAFE_CATALOG_PATH", privateCatalog)

	configPath, err := filepath.Abs("../../testdata/synthetic-acme/.limensafe/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	resolutions, err := cfg.ResolveCatalogs(configPath)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	r1 := resolutions[1]
	if r1.LoadStatus != "ok" || r1.Catalog == nil {
		t.Errorf("private catalog with env set: %+v", r1)
	}
	if r1.Catalog.CatalogID != "cs-spike-private-v0" {
		t.Errorf("private catalog id: got %q", r1.Catalog.CatalogID)
	}
}

func TestResolveCatalogs_RequiredCatalogIDMismatch(t *testing.T) {
	tmp := t.TempDir()
	otherCat := filepath.Join(tmp, "other.yaml")
	if err := os.WriteFile(otherCat, []byte(`
catalog_id: different-id
schema_version: "1.0.0"
entities:
  - {id: e-1, class: client_identity, aliases: [a]}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: expected-id
    source: {kind: file, path: other.yaml}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.ResolveCatalogs(configPath)
	if err == nil || !strings.Contains(err.Error(), "id mismatch") {
		t.Errorf("expected id mismatch error, got: %v", err)
	}
	for _, leak := range []string{"expected-id", "different-id"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("catalog id mismatch error leaked %q: %v", leak, err)
		}
	}
}

func TestResolveCatalogs_OptionalMissingFile(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: c-optional
    source: {kind: file, path: nonexistent.yaml}
    optional: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _ := LoadConfigFile(configPath)
	resolutions, err := cfg.ResolveCatalogs(configPath)
	if err != nil {
		t.Errorf("optional missing file should not error; got: %v", err)
	}
	if len(resolutions) != 1 || resolutions[0].LoadStatus != "absent_optional" {
		t.Errorf("expected absent_optional; got %+v", resolutions)
	}
}

func TestResolveCatalogs_RequiredMissingFile(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: c-required
    source: {kind: file, path: nonexistent.yaml}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _ := LoadConfigFile(configPath)
	_, err := cfg.ResolveCatalogs(configPath)
	if err == nil {
		t.Error("expected error for missing required catalog")
	}
	if strings.Contains(err.Error(), "c-required") {
		t.Fatalf("missing required error leaked catalog id: %v", err)
	}
}

func TestLoadBuiltin_PublicBaseline(t *testing.T) {
	c, err := LoadBuiltin(BuiltinPublicBaselineName)
	if err != nil {
		t.Fatal(err)
	}
	if c.CatalogID != "limensafe-public-baseline-v0" {
		t.Fatalf("catalog_id = %q", c.CatalogID)
	}
	if len(c.Entities) == 0 {
		t.Fatal("expected builtin entities")
	}
}

func TestLoadBuiltin_Unknown(t *testing.T) {
	_, err := LoadBuiltin("does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestLoadConfigBytes_BuiltinSourceRequiresName(t *testing.T) {
	yaml := `
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: limensafe-public-baseline-v0
    source: {kind: builtin}
`
	_, err := LoadConfigBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("expected name error, got: %v", err)
	}
}

func TestLoadConfigBytes_PrivateCatalogMissingValidation(t *testing.T) {
	yaml := `
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
policy:
  private_catalog_missing: noisy
catalogs:
  - catalog_id: limensafe-public-baseline-v0
    source: {kind: builtin, name: public-baseline}
`
	_, err := LoadConfigBytes([]byte(yaml))
	if err == nil || !strings.Contains(err.Error(), "private_catalog_missing") {
		t.Errorf("expected private_catalog_missing validation error, got: %v", err)
	}
}

func TestResolveCatalogs_BuiltinPublicBaseline(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: limensafe-public-baseline-v0
    source: {kind: builtin, name: public-baseline}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolutions, err := cfg.ResolveCatalogs(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(resolutions))
	}
	res := resolutions[0]
	if res.LoadStatus != "ok" || res.Catalog == nil {
		t.Fatalf("expected builtin catalog ok, got %+v", res)
	}
	if res.Source != BuiltinPublicBaselineName {
		t.Fatalf("source = %q, want %q", res.Source, BuiltinPublicBaselineName)
	}
}

func TestResolveCatalogs_BuiltinUnknownOptional(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: optional-builtin
    source: {kind: builtin, name: missing-baseline}
    optional: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolutions, err := cfg.ResolveCatalogs(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if resolutions[0].LoadStatus != "absent_optional" || resolutions[0].Catalog != nil {
		t.Fatalf("expected absent optional, got %+v", resolutions[0])
	}
}

func TestResolveCatalogs_PrivateCatalogMissingWarn(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: private-test
    source: {kind: env, var: LIMENSAFE_PRIVATE_TEST_CATALOG}
    optional: true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIMENSAFE_PRIVATE_TEST_CATALOG", "")

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolutions, err := cfg.ResolveCatalogsWithOptions(configPath, ResolveOptions{
		PrivateCatalogMissing: PrivateCatalogMissingWarn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(resolutions))
	}
	res := resolutions[0]
	if res.LoadStatus != LoadStatusMissingWarn || res.MissingReason != "env_unset" || res.SourceKind != "env" {
		t.Fatalf("warn resolution = %+v", res)
	}
}

func TestResolveCatalogs_PrivateCatalogMissingError(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`
schema_version: "1.0.0"
repo: {id: r, visibility: public_oss}
catalogs:
  - catalog_id: private-test
    source: {kind: env, var: LIMENSAFE_PRIVATE_TEST_CATALOG}
    optional: true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIMENSAFE_PRIVATE_TEST_CATALOG", "")

	cfg, err := LoadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolutions, err := cfg.ResolveCatalogsWithOptions(configPath, ResolveOptions{
		PrivateCatalogMissing: PrivateCatalogMissingError,
	})
	if err == nil {
		t.Fatal("expected error posture to fail")
	}
	if len(resolutions) != 1 || resolutions[0].LoadStatus != LoadStatusMissingError {
		t.Fatalf("error resolution = %+v", resolutions)
	}
	if strings.Contains(err.Error(), "LIMENSAFE_PRIVATE_TEST_CATALOG") {
		t.Fatalf("error leaked env var name: %v", err)
	}
	if strings.Contains(err.Error(), "private-test") {
		t.Fatalf("error leaked catalog id: %v", err)
	}
}
