package integration

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// This consumer fixture dispatches solely on the wire discriminator. Unknown
// versions fail before validation; a minor bump never selects an older schema.
func dispatchedSchema(root, kind, version string) (*jsonschema.Schema, error) {
	supported := false
	switch kind {
	case "scan":
		supported = version == "1.0.0" || version == "1.1.0" || version == "1.2.0"
	case "publish-surface":
		supported = version == "1.0.0" || version == "1.1.0"
	}
	if !supported {
		return nil, fmt.Errorf("unsupported output schema")
	}
	return jsonschema.Compile(filepath.Join(root, "schemas", "limensafe", "v"+version, kind+"-output.schema.json"))
}

func TestCoverageSchemaDispatchAndHistoricalScan(t *testing.T) {
	root := repoRootFromGoMod(t)
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		doc := emitMaximalDocument(t)
		meta := doc["scan_metadata"].(map[string]interface{})
		meta["output_schema_version"] = version
		if version != "1.2.0" {
			delete(doc["summary"].(map[string]interface{}), "coverage")
		}
		if version == "1.0.0" {
			delete(meta, "allowlist_suppressions")
			delete(meta, "allowlist_suppressions_by_entry")
		}
		sch, err := dispatchedSchema(root, "scan", version)
		if err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(doc); err != nil {
			t.Fatalf("historical %s: %v", version, err)
		}
	}
	current := emitMaximalDocument(t)
	old, err := dispatchedSchema(root, "scan", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Validate(current); err == nil {
		t.Fatal("old strict schema accepted new document")
	}
	for _, kind := range []string{"scan", "publish-surface"} {
		if _, err := dispatchedSchema(root, kind, "99.0.0"); err == nil {
			t.Fatal("unknown version accepted")
		}
	}
}

func TestCoverageSchemaRejectsMalformedMaps(t *testing.T) {
	sch := compileScanOutputSchema(t, repoRootFromGoMod(t))
	for _, mutate := range []func(map[string]interface{}){
		func(c map[string]interface{}) { delete(c, "status") },
		func(c map[string]interface{}) { c["skips_by_reason"] = nil },
		func(c map[string]interface{}) { c["allowances_by_reason"] = map[string]interface{}{"ignored": 1} },
		func(c map[string]interface{}) {
			c["non_budgetable_gaps_by_code"] = map[string]interface{}{"raw-error": 1}
		},
	} {
		doc := emitMaximalDocument(t)
		mutate(doc["summary"].(map[string]interface{})["coverage"].(map[string]interface{}))
		if err := sch.Validate(doc); err == nil {
			t.Fatal("malformed coverage accepted")
		}
	}
}
