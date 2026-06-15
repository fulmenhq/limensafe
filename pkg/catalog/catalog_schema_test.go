package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
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

func decodeCatalogYAMLForSchema(t *testing.T, data []byte) any {
	t.Helper()
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode yaml: %v", err)
	}
	normalized := normalizeYAMLForJSON(t, raw)
	jsonData, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("marshal normalized yaml as json: %v", err)
	}
	var doc any
	if err := json.Unmarshal(jsonData, &doc); err != nil {
		t.Fatalf("decode normalized json: %v", err)
	}
	return doc
}

func normalizeYAMLForJSON(t *testing.T, value any) any {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = normalizeYAMLForJSON(t, item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			key, ok := k.(string)
			if !ok {
				t.Fatalf("non-string YAML key %T=%v", k, k)
			}
			out[key] = normalizeYAMLForJSON(t, item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeYAMLForJSON(t, item)
		}
		return out
	case nil, string, bool, int, int64, float64:
		return v
	default:
		return fmt.Sprint(v)
	}
}
