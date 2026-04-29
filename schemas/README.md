# Schemas Directory

This directory contains JSON Schema definitions used for configuration validation.

## Directory Structure

Schemas follow the Fulmen catalog convention:

```
schemas/
└── {category}/
    └── {version}/
        └── {name}.schema.json
```

For limensafe:

```
schemas/
└── limensafe/
    └── v1.0.0/
        └── config.schema.json    # Configuration validation schema
```

## Supported JSONSchema Drafts

The schema validation system (via gofulmen/schema) supports all major JSONSchema drafts:

| Draft         | `$schema` URI                                  | Notes                                     |
| ------------- | ---------------------------------------------- | ----------------------------------------- |
| Draft-04      | `http://json-schema.org/draft-04/schema#`      | Legacy, widely supported                  |
| Draft-06      | `http://json-schema.org/draft-06/schema#`      | Added `const`, `contains`                 |
| Draft-07      | `http://json-schema.org/draft-07/schema#`      | Added `if`/`then`/`else`, `$comment`      |
| Draft 2019-09 | `https://json-schema.org/draft/2019-09/schema` | Added `unevaluatedProperties`, vocabulary |
| Draft 2020-12 | `https://json-schema.org/draft/2020-12/schema` | Current, added `prefixItems`              |

**Recommendation**: Use Draft 2020-12 for new schemas. Use older drafts only when integrating with external systems that require them.

## Draft Auto-Detection

The validator automatically detects the draft from the `$schema` field in your schema. No configuration needed:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "myapp/v1.0.0/config",
  "type": "object",
  "properties": { ... }
}
```

## For CDRL Users

When refitting this template, you can:

1. **Keep the existing schema** - Update `schemas/limensafe/v1.0.0/config.schema.json` with your app's config structure
2. **Rename the category** - Move to `schemas/{your-app}/v1.0.0/`
3. **Add additional schemas** - For API payloads, events, etc.
4. **Use any supported draft** - Mix drafts as needed for external integrations

Example after refit:

```
schemas/
├── myapp/
│   └── v1.0.0/
│       ├── config.schema.json        # Your app config (Draft 2020-12)
│       └── events.schema.json        # Event payloads (Draft 2020-12)
└── external/
    └── v2.0.0/
        └── partner-api.schema.json   # Third-party schema (Draft-07)
```

## Schema Catalog Usage

Load schemas via the catalog in your code:

```go
import "github.com/fulmenhq/gofulmen/schema"

// Create catalog from schemas directory
catalog := schema.NewCatalog("./schemas")

// Get schema descriptor (includes draft info)
desc, err := catalog.GetSchema("myapp/v1.0.0/config")

// Validate data against schema
diags, err := catalog.ValidateDataByID("myapp/v1.0.0/config", jsonData)
```

## Meta-Validation

Validate your schemas are well-formed before deployment:

```go
schemaBytes, _ := os.ReadFile("schemas/myapp/v1.0.0/config.schema.json")
diags, err := schema.ValidateSchemaBytes(schemaBytes)
if len(diags) > 0 {
    // Schema has issues
}
```

## See Also

- [Schema Validation Guide](../docs/schema-validation.md) - Detailed usage documentation
- [gofulmen/schema](https://github.com/fulmenhq/gofulmen) - Schema validation library
- [JSON Schema](https://json-schema.org/) - Official specification
