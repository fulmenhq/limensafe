package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	schemaassets "github.com/fulmenhq/limensafe/internal/assets/schemas"
)

// SupportedCatalogSchemaVersion is the catalog schema contract version this
// build was authored against. Catalogs declaring a higher minor/patch within
// the same major still load, with an advisory LoadWarning; a higher major is a
// hard config error (exit 2).
//
// 1.1.0 (internal-brief): adds the optional catalog-level `allowlist` primitive. The
// addition is backward compatible — 1.0.x catalogs validate unchanged against
// the shared v1 structural schema — so this is a minor bump.
const SupportedCatalogSchemaVersion = "1.1.0"

const catalogSchemaURI = "https://schemas.fulmenhq.dev/limensafe/v1/catalog.schema.json"

var (
	catalogSchemaOnce sync.Once
	catalogSchema     *jsonschema.Schema
	catalogSchemaErr  error
)

// compiledCatalogSchema compiles the embedded catalog JSON Schema once. The
// schema is embedded (not read from disk) so runtime validation behaves
// identically in-repo and in an installed binary.
func compiledCatalogSchema() (*jsonschema.Schema, error) {
	catalogSchemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(catalogSchemaURI, bytes.NewReader(schemaassets.CatalogSchema)); err != nil {
			catalogSchemaErr = fmt.Errorf("compile embedded catalog schema: %w", err)
			return
		}
		catalogSchema, catalogSchemaErr = compiler.Compile(catalogSchemaURI)
	})
	return catalogSchema, catalogSchemaErr
}

// validateAgainstSchema validates the raw catalog bytes against the embedded
// JSON Schema. The returned error is redaction-safe by construction: it carries
// only JSON-pointer instance locations and failing-keyword names, never the
// offending instance value, so no protected vocabulary can leak onto any
// stream. The library's free-text message is deliberately discarded.
func validateAgainstSchema(data []byte) error {
	schema, err := compiledCatalogSchema()
	if err != nil {
		return err
	}
	doc, err := decodeYAMLAsJSONDoc(data)
	if err != nil {
		// Value-free: a decode/normalize error could otherwise wrap library
		// text quoting catalog content (ADR-0003). The raw bytes already
		// parsed into the catalog struct upstream, so this is defensive.
		return fmt.Errorf("catalog could not be normalized for schema validation")
	}
	if err := schema.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if as, ok := err.(*jsonschema.ValidationError); ok {
			ve = as
		}
		if ve == nil {
			// Non-validation error (e.g. unsupported instance type). Report
			// the shape without echoing content.
			return fmt.Errorf("schema validation failed")
		}
		return fmt.Errorf("schema validation failed: %s", summarizeSchemaViolations(ve))
	}
	return nil
}

// validateKnownCatalogAuthoringMistakes recognizes common pre-schema authoring
// slips and returns value-free diagnostics. It deliberately matches only fixed
// known keys instead of echoing arbitrary operator-supplied YAML field names:
// this runs before a catalog redactor can exist.
func validateKnownCatalogAuthoringMistakes(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	root := documentRoot(&doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	entities := mappingValue(root, "entities")
	if entities == nil || entities.Kind != yaml.SequenceNode {
		return nil
	}
	for i, entity := range entities.Content {
		if entity == nil || entity.Kind != yaml.MappingNode {
			continue
		}
		if mappingValue(entity, "match") != nil {
			return fmt.Errorf("entity[%d]: unknown field match; did you mean regex_patterns?", i)
		}
	}
	return nil
}

func documentRoot(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		if k != nil && k.Kind == yaml.ScalarNode && k.Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// summarizeSchemaViolations renders a deterministic, redaction-safe summary of
// the failing locations and keywords. It walks to the leaf causes and emits
// only (instance pointer, keyword) pairs.
func summarizeSchemaViolations(root *jsonschema.ValidationError) string {
	seen := map[string]struct{}{}
	var items []string
	var walk func(ve *jsonschema.ValidationError)
	walk = func(ve *jsonschema.ValidationError) {
		if len(ve.Causes) > 0 {
			for _, c := range ve.Causes {
				walk(c)
			}
			return
		}
		loc := ve.InstanceLocation
		if loc == "" {
			loc = "(root)"
		}
		item := fmt.Sprintf("%s: %s", loc, keywordOf(ve.KeywordLocation))
		if _, dup := seen[item]; dup {
			return
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	walk(root)
	sort.Strings(items)
	return strings.Join(items, "; ")
}

// keywordOf returns the trailing keyword name from a JSON-pointer keyword
// location (e.g. ".../entities/items/required" -> "required"). The keyword name
// is schema-side and never carries instance content.
func keywordOf(keywordLocation string) string {
	kl := strings.TrimRight(keywordLocation, "/")
	if i := strings.LastIndex(kl, "/"); i >= 0 && i+1 < len(kl) {
		return kl[i+1:]
	}
	if kl == "" {
		return "schema"
	}
	return kl
}

// schemaVersionPolicy enforces the catalog schema_version compatibility policy.
// A non-1 major (or an unparseable version) is a hard config error; a higher
// minor/patch within major 1 returns an advisory warning and loads.
func schemaVersionPolicy(version string) (warning string, err error) {
	major, minor, patch, perr := parseSemverCore(version)
	if perr != nil {
		// Value-free by design: schema_version is operator-controlled and a
		// malformed catalog could place protected vocabulary here. This policy
		// runs before the schema pointer+keyword renderer, so it must not echo
		// the offending value (ADR-0003 zero-leak boundary).
		return "", fmt.Errorf("schema_version is not a valid semantic version; expected major.minor.patch")
	}
	if major != 1 {
		return "", fmt.Errorf("schema_version major %d is unsupported; this build supports catalog schema v1 (declare 1.x)", major)
	}
	sMajor, sMinor, sPatch, _ := parseSemverCore(SupportedCatalogSchemaVersion)
	_ = sMajor
	if minor > sMinor || (minor == sMinor && patch > sPatch) {
		return fmt.Sprintf(
			"catalog declares schema_version %d.%d.%d, newer than this build's supported %s; loading anyway, upgrade limensafe if validation surprises you",
			major, minor, patch, SupportedCatalogSchemaVersion,
		), nil
	}
	return "", nil
}

// schemaURIWarning returns an advisory warning when a catalog omits $schema.
// $schema is optional during the v0.1.x compatibility window and becomes
// required from v0.2.0.
func schemaURIWarning(schemaURL string) string {
	if strings.TrimSpace(schemaURL) == "" {
		return "catalog omits $schema; it is optional during the v0.1.x window but will be required from v0.2.0 (set it to " + catalogSchemaURI + ")"
	}
	return ""
}

// parseSemverCore extracts the numeric major.minor.patch, ignoring any
// pre-release/build metadata suffix.
func parseSemverCore(version string) (major, minor, patch int, err error) {
	core := version
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("expected major.minor.patch")
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, convErr := strconv.Atoi(p)
		if convErr != nil || n < 0 {
			return 0, 0, 0, fmt.Errorf("non-numeric version component")
		}
		out[i] = n
	}
	return out[0], out[1], out[2], nil
}

// decodeYAMLAsJSONDoc decodes catalog YAML into a JSON-native document
// (map[string]any / []any / float64 / string / bool / nil) so the JSON Schema
// validator sees the types it expects regardless of YAML's native typing.
func decodeYAMLAsJSONDoc(data []byte) (any, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	normalized, err := normalizeYAMLForJSON(raw)
	if err != nil {
		return nil, err
	}
	jsonData, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
	}
	var doc any
	if err := json.Unmarshal(jsonData, &doc); err != nil {
		return nil, fmt.Errorf("normalize: %w", err)
	}
	return doc, nil
}

// normalizeYAMLForJSON converts YAML map keys to strings so the value can be
// marshaled as JSON. yaml.v3 already yields map[string]any for mappings; the
// map[any]any branch is defensive for any nested decoder behavior.
func normalizeYAMLForJSON(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			nv, err := normalizeYAMLForJSON(item)
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			key, ok := k.(string)
			if !ok {
				key = fmt.Sprintf("%v", k)
			}
			nv, err := normalizeYAMLForJSON(item)
			if err != nil {
				return nil, err
			}
			out[key] = nv
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			nv, err := normalizeYAMLForJSON(item)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	default:
		return v, nil
	}
}
