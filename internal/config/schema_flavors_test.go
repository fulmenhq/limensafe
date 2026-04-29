// Package config provides centralized configuration management for the Limensafe service.
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fulmenhq/gofulmen/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSchemaFlavors_AllDraftsSupported verifies that gofulmen/schema correctly
// compiles and validates schemas across all supported JSONSchema drafts.
//
// This test demonstrates to CDRL users that they can use any of these draft
// versions for their own schemas - the validation infrastructure supports them all.
//
// Supported drafts:
//   - Draft-04: http://json-schema.org/draft-04/schema#
//   - Draft-06: http://json-schema.org/draft-06/schema#
//   - Draft-07: http://json-schema.org/draft-07/schema#
//   - Draft 2019-09: https://json-schema.org/draft/2019-09/schema
//   - Draft 2020-12: https://json-schema.org/draft/2020-12/schema
func TestSchemaFlavors_AllDraftsSupported(t *testing.T) {
	projectRoot, err := findProjectRoot()
	require.NoError(t, err, "failed to find project root")

	testdataDir := filepath.Join(projectRoot, "testdata", "schemas")

	testCases := []struct {
		name        string
		schemaFile  string
		validData   string
		invalidData string
	}{
		{
			name:        "Draft-04",
			schemaFile:  "draft-04-sample.schema.json",
			validData:   `{"name": "test", "count": 5, "enabled": true}`,
			invalidData: `{"name": "", "count": -1}`,
		},
		{
			name:        "Draft-06",
			schemaFile:  "draft-06-sample.schema.json",
			validData:   `{"name": "test", "count": 5, "enabled": true}`,
			invalidData: `{"name": "test", "enabled": false}`,
		},
		{
			name:        "Draft-07",
			schemaFile:  "draft-07-sample.schema.json",
			validData:   `{"name": "test", "count": 5, "enabled": true}`,
			invalidData: `{"name": ""}`,
		},
		{
			name:        "Draft-2019-09",
			schemaFile:  "draft-2019-09-sample.schema.json",
			validData:   `{"name": "test", "count": 5, "enabled": true}`,
			invalidData: `{"count": 5}`,
		},
		{
			name:        "Draft-2020-12",
			schemaFile:  "draft-2020-12-sample.schema.json",
			validData:   `{"name": "test", "count": 5, "enabled": true}`,
			invalidData: `{"name": 123}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			schemaPath := filepath.Join(testdataDir, tc.schemaFile)

			// Load schema bytes
			schemaBytes, err := os.ReadFile(schemaPath)
			require.NoError(t, err, "failed to read schema file: %s", tc.schemaFile)

			// Compile schema - this verifies the draft is recognized
			validator, err := schema.NewValidator(schemaBytes)
			require.NoError(t, err, "failed to compile %s schema", tc.name)
			require.NotNil(t, validator, "validator should not be nil")

			// Validate good data
			t.Run("valid_data", func(t *testing.T) {
				diags, err := validator.ValidateJSON([]byte(tc.validData))
				require.NoError(t, err, "validation should not error")
				assert.Empty(t, diags, "valid data should produce no diagnostics")
			})

			// Validate bad data
			t.Run("invalid_data", func(t *testing.T) {
				diags, err := validator.ValidateJSON([]byte(tc.invalidData))
				require.NoError(t, err, "validation should not error even for invalid data")
				assert.NotEmpty(t, diags, "invalid data should produce diagnostics")
			})
		})
	}
}

// TestSchemaMetaValidation verifies that schemas themselves can be validated
// (meta-validation). This is useful for CDRL users who want to validate their
// custom schemas are well-formed before deploying.
func TestSchemaMetaValidation(t *testing.T) {
	projectRoot, err := findProjectRoot()
	require.NoError(t, err, "failed to find project root")

	testdataDir := filepath.Join(projectRoot, "testdata", "schemas")

	schemaFiles := []string{
		"draft-04-sample.schema.json",
		"draft-06-sample.schema.json",
		"draft-07-sample.schema.json",
		"draft-2019-09-sample.schema.json",
		"draft-2020-12-sample.schema.json",
	}

	for _, schemaFile := range schemaFiles {
		t.Run(schemaFile, func(t *testing.T) {
			schemaPath := filepath.Join(testdataDir, schemaFile)

			// Read schema bytes
			schemaBytes, err := os.ReadFile(schemaPath)
			require.NoError(t, err, "failed to read schema file")

			// Meta-validate the schema itself
			diags, err := schema.ValidateSchemaBytes(schemaBytes)
			require.NoError(t, err, "meta-validation should not error")
			assert.Empty(t, diags, "schema should be valid according to its meta-schema")
		})
	}
}

// TestSchemaMetaValidation_InvalidSchema verifies that meta-validation catches
// malformed schemas.
func TestSchemaMetaValidation_InvalidSchema(t *testing.T) {
	// Schema with invalid type value
	invalidSchema := []byte(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "not-a-valid-type"
	}`)

	diags, err := schema.ValidateSchemaBytes(invalidSchema)
	require.NoError(t, err, "meta-validation should not error")
	assert.NotEmpty(t, diags, "invalid schema should produce diagnostics")
}

// TestSchemaCatalog_StandardStructure demonstrates using a schema catalog
// with the standard Fulmen directory structure: {category}/{version}/{name}.schema.json
//
// CDRL users should organize their schemas following this pattern:
//
//	schemas/
//	├── myapp/
//	│   └── v1.0.0/
//	│       ├── config.schema.json      (Draft 2020-12)
//	│       └── events.schema.json      (any supported draft)
//	└── external/
//	    └── v2.0.0/
//	        └── legacy-api.schema.json  (Draft-07 from third party)
//
// The catalog auto-detects the draft from the $schema field.
func TestSchemaCatalog_StandardStructure(t *testing.T) {
	projectRoot, err := findProjectRoot()
	require.NoError(t, err, "failed to find project root")

	schemasDir := filepath.Join(projectRoot, "schemas")

	// Create a catalog from the schemas directory
	catalog := schema.NewCatalog(schemasDir)
	require.NotNil(t, catalog, "catalog should not be nil")

	// Get the limensafe config schema by ID
	descriptor, err := catalog.GetSchema("limensafe/v1.0.0/config")
	require.NoError(t, err, "should find limensafe config schema")
	assert.Equal(t, "limensafe/v1.0.0/config", descriptor.ID)
	assert.Contains(t, descriptor.Draft, "2020-12", "limensafe uses Draft 2020-12")

	t.Logf("Found schema: ID=%s, Draft=%s", descriptor.ID, descriptor.Draft)

	// Validate data using the catalog
	validConfig := []byte(`{"server": {"port": 8080}, "logging": {"level": "info"}}`)
	diags, err := catalog.ValidateDataByID("limensafe/v1.0.0/config", validConfig)
	require.NoError(t, err, "validation should not error")
	assert.Empty(t, diags, "valid config should produce no diagnostics")
}
