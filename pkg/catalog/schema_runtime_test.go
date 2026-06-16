package catalog

import (
	"strings"
	"testing"
)

const schemaURILine = `$schema: "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"`

// TestLoadBytes_MajorVersionMismatchIsConfigError confirms a non-1 major is a
// hard load error (mapped to exit 2 at the command layer) with an actionable,
// redaction-safe message.
func TestLoadBytes_MajorVersionMismatchIsConfigError(t *testing.T) {
	yaml := schemaURILine + `
catalog_id: cs-major
schema_version: "2.0.0"
entities:
  - id: e-1
    class: codename
    aliases: ["alpha"]
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected major-version mismatch to be a load error")
	}
	if !strings.Contains(err.Error(), "major") || !strings.Contains(err.Error(), "v1") {
		t.Fatalf("expected actionable major-version message, got: %v", err)
	}
}

// TestLoadBytes_ForwardMinorVersionWarns confirms a higher minor within major 1
// loads and surfaces an advisory LoadWarning rather than failing.
func TestLoadBytes_ForwardMinorVersionWarns(t *testing.T) {
	yaml := schemaURILine + `
catalog_id: cs-minor
schema_version: "1.99.0"
entities:
  - id: e-1
    class: codename
    aliases: ["alpha"]
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("forward-minor catalog should load, got: %v", err)
	}
	if !hasWarningContaining(c.Warnings, "newer than this build") {
		t.Fatalf("expected forward-minor warning, got: %#v", c.Warnings)
	}
}

// TestLoadBytes_MissingSchemaURIWarns confirms the v0.1.x compatibility-window
// advisory fires when $schema is omitted.
func TestLoadBytes_MissingSchemaURIWarns(t *testing.T) {
	yaml := `
catalog_id: cs-no-schema
schema_version: "1.0.0"
entities:
  - id: e-1
    class: codename
    aliases: ["alpha"]
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("catalog omitting $schema should still load in v0.1.x, got: %v", err)
	}
	if !hasWarningContaining(c.Warnings, "omits $schema") {
		t.Fatalf("expected missing-$schema warning, got: %#v", c.Warnings)
	}
}

// TestLoadBytes_PresentSchemaURINoWarning confirms a conformant catalog with
// $schema present does not emit the compatibility advisory.
func TestLoadBytes_PresentSchemaURINoWarning(t *testing.T) {
	yaml := schemaURILine + `
catalog_id: cs-schema
schema_version: "1.0.0"
entities:
  - id: e-1
    class: codename
    aliases: ["alpha"]
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasWarningContaining(c.Warnings, "omits $schema") {
		t.Fatalf("did not expect missing-$schema warning, got: %#v", c.Warnings)
	}
}

// TestLoadBytes_SchemaDiagnosticsAreRedactionSafe is the zero-leak guarantee for
// the runtime validator: a schema violation on a field that holds protected
// vocabulary must not echo that vocabulary on any stream. A duplicate protected
// alias trips uniqueItems; the diagnostic must carry pointer+keyword only.
func TestLoadBytes_SchemaDiagnosticsAreRedactionSafe(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	yaml := schemaURILine + `
catalog_id: cs-leak
schema_version: "1.0.0"
entities:
  - id: e-1
    class: codename
    aliases: ["` + protected + `", "` + protected + `"]
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected duplicate-alias schema violation")
	}
	if strings.Contains(err.Error(), protected) || strings.Contains(err.Error(), strings.ToLower(protected)) {
		t.Fatalf("schema diagnostic leaked protected alias: %v", err)
	}
	if !strings.Contains(err.Error(), "uniqueItems") {
		t.Fatalf("expected uniqueItems keyword in diagnostic, got: %v", err)
	}
}

// TestLoadBytes_MalformedVersionDiagnosticIsRedactionSafe guards the
// pre-schema version-policy path (secrev finding): schema_version is
// operator-controlled, so an unparseable value must not be echoed even though
// the policy runs before the JSON Schema pointer+keyword renderer. The
// protected sentinel appears here both as an alias and as the invalid version.
func TestLoadBytes_MalformedVersionDiagnosticIsRedactionSafe(t *testing.T) {
	const protected = "SUPERSECRETCODENAME"
	yaml := schemaURILine + `
catalog_id: cs-badver
schema_version: "` + protected + `"
entities:
  - id: e-1
    class: codename
    aliases: ["` + protected + `"]
`
	_, err := LoadBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected malformed schema_version to be a load error")
	}
	if strings.Contains(err.Error(), protected) || strings.Contains(err.Error(), strings.ToLower(protected)) {
		t.Fatalf("version diagnostic leaked protected value: %v", err)
	}
	if !strings.Contains(err.Error(), "semantic version") {
		t.Fatalf("expected semantic-version reason, got: %v", err)
	}
}

// TestLoadBytes_FingerprintSaltCatalogValidates pins fingerprint_salt as a
// loadable, schema-valid field (cxotech review nit: it was the least-trodden
// catalog field with no positive conformance coverage).
func TestLoadBytes_FingerprintSaltCatalogValidates(t *testing.T) {
	yaml := schemaURILine + `
catalog_id: cs-salt
schema_version: "1.0.0"
fingerprint_salt: "static-salt-value"
entities:
  - id: e-1
    class: codename
    aliases: ["alpha"]
    tokens: ["BRAVO"]
`
	c, err := LoadBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("fingerprint_salt catalog should load, got: %v", err)
	}
	if c.FingerprintSalt != "static-salt-value" {
		t.Fatalf("fingerprint_salt = %q, want static-salt-value", c.FingerprintSalt)
	}
}

func hasWarningContaining(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
