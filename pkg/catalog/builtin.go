package catalog

import (
	"embed"
	"fmt"
)

const BuiltinPublicBaselineName = "public-baseline"

//go:embed builtin/*.yaml
var builtinCatalogFS embed.FS

var builtinCatalogFiles = map[string]string{
	BuiltinPublicBaselineName: "builtin/public-baseline.yaml",
}

// LoadBuiltin loads a catalog bundled into the limensafe binary.
// Built-in catalogs are opt-in and must contain only public-safe patterns.
func LoadBuiltin(name string) (*Catalog, error) {
	path, ok := builtinCatalogFiles[name]
	if !ok {
		return nil, fmt.Errorf("builtin catalog %q not found", name)
	}
	data, err := builtinCatalogFS.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("builtin catalog %q: %w", name, err)
	}
	c, err := LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("builtin catalog %q: %w", name, err)
	}
	return c, nil
}
