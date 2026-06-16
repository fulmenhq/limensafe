// Package schemaassets exposes limensafe JSON Schema contracts as embedded
// bytes so the runtime validator works in a standalone binary, with no
// dependency on the repository's on-disk schemas/ tree.
//
// The embedded copies are mirrored from the canonical schemas/ files and are
// kept in sync via `make sync-embedded-schemas`. `make verify-embedded-schemas`
// fails the build if the mirror drifts from canonical, so the runtime can
// never validate against a stale contract.
package schemaassets

import _ "embed"

// CatalogSchema is the embedded copy of
// schemas/limensafe/v1/catalog.schema.json (draft 2020-12). It is the
// structural contract the catalog loader validates operator catalogs against
// at scan time.
//
//go:embed limensafe/v1/catalog.schema.json
var CatalogSchema []byte
