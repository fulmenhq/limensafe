package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestCatalogJSONSchema_ValidatesInRepoCatalogs(t *testing.T) {
	schema := compileCatalogSchema(t)
	for _, path := range []string{
		filepath.Join("..", "..", "pkg", "catalog", "builtin", "public-baseline.yaml"),
		filepath.Join("..", "..", "testdata", "synthetic-acme", "catalog", "synthetic-acme-public.catalog.yaml"),
		filepath.Join("..", "..", "testdata", "synthetic-acme", "catalog", "synthetic-acme.catalog.yaml"),
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			doc := loadCatalogYAMLForSchema(t, path)
			if err := schema.Validate(doc); err != nil {
				t.Fatalf("catalog %s failed schema validation: %v", path, err)
			}
		})
	}
}

func TestCatalogJSONSchema_StructuralViolations(t *testing.T) {
	schema := compileCatalogSchema(t)
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing_entities",
			yaml: `
catalog_id: cs-test
schema_version: "1.0.0"
`,
		},
		{
			name: "major_version_mismatch",
			yaml: `
catalog_id: cs-test
schema_version: "2.0.0"
entities:
  - id: e-one
    class: codename
    aliases: ["alpha"]
`,
		},
		{
			name: "unknown_top_level_field",
			yaml: `
catalog_id: cs-test
schema_version: "1.0.0"
owner_email: operator@example.test
entities:
  - id: e-one
    class: codename
    aliases: ["alpha"]
`,
		},
		{
			name: "bad_severity_enum",
			yaml: `
catalog_id: cs-test
schema_version: "1.0.0"
default_severity: urgent
entities:
  - id: e-one
    class: codename
    aliases: ["alpha"]
`,
		},
		{
			name: "entity_without_alias_or_regex",
			yaml: `
catalog_id: cs-test
schema_version: "1.0.0"
entities:
  - id: e-one
    class: codename
    aliases: []
`,
		},
		{
			name: "near_window_missing_size",
			yaml: `
catalog_id: cs-test
schema_version: "1.0.0"
entities:
  - id: e-one
    class: codename
    aliases: ["alpha"]
  - id: e-two
    class: project
    aliases: ["beta"]
co_occurrence_rules:
  - rule_id: r-one
    terms: [e-one, e-two]
    window_kind: near_n_chars
    severity_override: critical
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := decodeCatalogYAMLForSchema(t, []byte(tt.yaml))
			if err := schema.Validate(doc); err == nil {
				t.Fatal("expected schema validation to fail, but it passed")
			}
		})
	}
}

func TestCatalogJSONSchema_MissingSchemaURIAllowedForCompatWindow(t *testing.T) {
	schema := compileCatalogSchema(t)
	doc := decodeCatalogYAMLForSchema(t, []byte(`
catalog_id: cs-test
schema_version: "1.0.0"
entities:
  - id: e-one
    class: codename
    aliases: ["alpha"]
`))
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("missing $schema should validate during v0.1.x compatibility window: %v", err)
	}
}

func TestCatalogJSONSchema_TokenOnlyEntityValidates(t *testing.T) {
	schema := compileCatalogSchema(t)
	doc := decodeCatalogYAMLForSchema(t, []byte(`
catalog_id: cs-test
schema_version: "1.0.0"
entities:
  - id: e-token
    class: operational_pattern
    tokens: ["ILT"]
`))
	if err := schema.Validate(doc); err != nil {
		t.Fatalf("token-only entity should validate: %v", err)
	}
}

func compileCatalogSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "limensafe", "v1", "catalog.schema.json"))
	if err != nil {
		t.Fatalf("read catalog schema: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("catalog.schema.json", bytes.NewReader(data)); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile("catalog.schema.json")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	return schema
}

func loadCatalogYAMLForSchema(t *testing.T, path string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return decodeCatalogYAMLForSchema(t, data)
}

// decodeCatalogYAMLForSchema delegates to the production YAML→JSON normalizer
// so the conformance tests exercise the exact decode path the runtime loader
// uses (see decodeYAMLAsJSONDoc in schema.go).
func decodeCatalogYAMLForSchema(t *testing.T, data []byte) any {
	t.Helper()
	doc, err := decodeYAMLAsJSONDoc(data)
	if err != nil {
		t.Fatalf("decode catalog yaml: %v", err)
	}
	return doc
}
