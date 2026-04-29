# Schema Validation Guide

Limensafe includes comprehensive JSON Schema validation for configuration and data payloads. This guide covers the validation capabilities available to CDRL users.

## Overview

Schema validation is provided by [gofulmen/schema](https://github.com/fulmenhq/gofulmen), which wraps the proven [santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema) library with Fulmen conventions.

**Key capabilities:**

- Support for all major JSONSchema drafts (Draft-04 through Draft 2020-12)
- Automatic draft detection from `$schema` field
- Meta-validation (validate schemas themselves)
- Schema catalog for organized schema management
- Integration with gofulmen/config for configuration validation

## Supported Drafts

| Draft         | `$schema` URI                                  | Introduced |
| ------------- | ---------------------------------------------- | ---------- |
| Draft-04      | `http://json-schema.org/draft-04/schema#`      | 2013       |
| Draft-06      | `http://json-schema.org/draft-06/schema#`      | 2017       |
| Draft-07      | `http://json-schema.org/draft-07/schema#`      | 2018       |
| Draft 2019-09 | `https://json-schema.org/draft/2019-09/schema` | 2019       |
| Draft 2020-12 | `https://json-schema.org/draft/2020-12/schema` | 2020       |

The draft is auto-detected from the `$schema` field - no configuration required.

## Quick Start

### Validate Data Against a Schema

```go
import "github.com/fulmenhq/gofulmen/schema"

// Load your schema
schemaBytes, err := os.ReadFile("schemas/myapp/v1.0.0/config.schema.json")
if err != nil {
    return err
}

// Create validator (draft auto-detected)
validator, err := schema.NewValidator(schemaBytes)
if err != nil {
    return fmt.Errorf("invalid schema: %w", err)
}

// Validate JSON data
data := []byte(`{"name": "test", "port": 8080}`)
diags, err := validator.ValidateJSON(data)
if err != nil {
    return fmt.Errorf("validation error: %w", err)
}

if len(diags) > 0 {
    for _, d := range diags {
        log.Printf("Validation issue at %s: %s", d.Pointer, d.Message)
    }
}
```

### Using the Schema Catalog

For multiple schemas, use the catalog:

```go
import "github.com/fulmenhq/gofulmen/schema"

// Create catalog from schemas directory
catalog := schema.NewCatalog("./schemas")

// Get schema metadata
desc, err := catalog.GetSchema("myapp/v1.0.0/config")
if err != nil {
    return err
}
log.Printf("Schema draft: %s", desc.Draft)

// Validate directly by schema ID
diags, err := catalog.ValidateDataByID("myapp/v1.0.0/config", jsonData)
```

### Meta-Validation (Validate Your Schemas)

Ensure your schemas are well-formed before deployment:

```go
import "github.com/fulmenhq/gofulmen/schema"

schemaBytes, _ := os.ReadFile("schemas/myapp/v1.0.0/config.schema.json")

// Validate the schema against its meta-schema
diags, err := schema.ValidateSchemaBytes(schemaBytes)
if err != nil {
    return fmt.Errorf("meta-validation failed: %w", err)
}

if len(diags) > 0 {
    for _, d := range diags {
        log.Printf("Schema issue: %s", d.Message)
    }
    return errors.New("schema is invalid")
}
```

## Configuration Validation

Limensafe's config loader automatically validates configuration against the schema:

```go
// internal/config/loader.go pattern
catalog := schema.NewCatalog(filepath.Join(projectRoot, "schemas"))
opts := gfconfig.LayeredConfigOptions{
    Category:     "limensafe",
    Version:      "v1.0.0",
    SchemaID:     "limensafe/v1.0.0/config",
    Catalog:      catalog,
    // ...
}
```

Invalid configuration prevents startup, ensuring runtime safety.

## Schema Directory Structure

Follow the Fulmen catalog convention:

```
schemas/
└── {category}/
    └── {version}/
        └── {name}.schema.json
```

The schema ID is derived from the path: `{category}/{version}/{name}`

## Draft Selection Guidelines

| Use Case                    | Recommended Draft |
| --------------------------- | ----------------- |
| New schemas                 | Draft 2020-12     |
| Maximum compatibility       | Draft-07          |
| External/legacy integration | Match the source  |

**Draft 2020-12** is recommended for new schemas as it's the current standard with the best tooling support.

## Diagnostics

Validation returns `[]Diagnostic` with structured error information:

```go
type Diagnostic struct {
    Pointer  string        // JSON Pointer to the error location
    Message  string        // Human-readable error message
    Severity SeverityLevel // ERROR, WARNING, INFO
}
```

Convert to simple strings if needed:

```go
errors := schema.DiagnosticsToStringSlice(diags)
```

## Testing Schemas

See `internal/config/schema_flavors_test.go` for examples of:

- Testing schema compilation across all drafts
- Validating good and bad data
- Meta-validation patterns
- Catalog usage

## See Also

- [schemas/README.md](../schemas/README.md) - Schema directory documentation
- [gofulmen/schema](https://github.com/fulmenhq/gofulmen) - Underlying library
- [JSON Schema Specification](https://json-schema.org/specification)
