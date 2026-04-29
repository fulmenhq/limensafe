# ADR 0001: Repository Root Detection for Config Loading

**Status:** Accepted

**Date:** 2025-11-15

**Deciders:** @3leapsdave (devlead)

**Context:** Feature Brief 007 (gofulmen/config Integration)

---

## Context and Problem Statement

As a **Layer 2 template**, forge-workhorse-limensafe ships with its own configuration defaults and schemas located at:

- `config/limensafe/v1.0.0/limensafe-defaults.yaml`
- `schemas/limensafe/v1.0.0/config.schema.json`

When loading configuration via `gofulmen/config.LoadLayeredConfig()`, we need to provide absolute paths to both the defaults directory and schema catalog. However, the process working directory varies:

- **CLI execution**: Runs from project root
- **Tests**: Run from `internal/config/` or other subdirectories
- **Integration tests**: May run from various locations

The problem: How do we reliably locate the project root to construct absolute paths to `config/` and `schemas/` directories?

## Decision Drivers

1. **Test Reliability**: Tests must work regardless of working directory
2. **Developer Experience**: Should work from any directory (supports `go test ./...` patterns)
3. **Layer 2 Pattern**: Templates house defaults locally (not synced from Crucible SSOT)
4. **Simplicity**: Avoid complex environment variable configuration
5. **Future-Proofing**: Should be easy to replace with gofulmen library function when available

## Considered Options

### Option 1: Relative Paths (REJECTED)

```go
opts := gfconfig.LayeredConfigOptions{
    DefaultsRoot: "config",
    Catalog:      schema.NewCatalog("schemas"),
}
```

**Pros:**

- Simple implementation
- No upward traversal needed

**Cons:**

- ❌ Fails when tests run from subdirectories
- ❌ Requires tests to always `cd` to project root first
- ❌ Breaks `go test ./internal/config/...` pattern

**Verdict:** Not viable for robust test execution

### Option 2: Environment Variable (REJECTED)

```go
projectRoot := os.Getenv("PROJECT_ROOT")
if projectRoot == "" {
    projectRoot = "." // fallback
}
```

**Pros:**

- Explicit configuration
- Easy to override

**Cons:**

- ❌ Requires developers to set env var
- ❌ Breaks out-of-the-box experience
- ❌ Extra configuration burden for tests
- ❌ Not idiomatic for Go projects

**Verdict:** Too much configuration burden

### Option 3: Custom findProjectRoot() Helper (ACCEPTED) ✅

```go
func findProjectRoot() (string, error) {
    cwd, err := os.Getwd()
    if err != nil {
        return "", fmt.Errorf("failed to get current directory: %w", err)
    }

    current := cwd
    for i := 0; i < 10; i++ { // Search up to 10 levels
        // Check for project markers
        if fileExists(filepath.Join(current, "go.mod")) {
            return current, nil
        }
        if fileExists(filepath.Join(current, ".git")) {
            return current, nil
        }

        // Move up one directory
        parent := filepath.Dir(current)
        if parent == current {
            // Reached filesystem root
            break
        }
        current = parent
    }

    return "", fmt.Errorf("project root not found (no go.mod or .git found)")
}
```

**Pros:**

- ✅ Works from any working directory
- ✅ Zero configuration required
- ✅ Follows common Go patterns (similar to go.mod discovery)
- ✅ Mirrors gofulmen/schema catalog's upward search pattern
- ✅ Easy to replace with library function later

**Cons:**

- Adds ~10 filesystem checks on config load
- Could fail in unusual project structures (no go.mod or .git)

**Verdict:** Best balance of reliability and simplicity

### Option 4: Go Build Embed (REJECTED)

```go
//go:embed config/limensafe/v1.0.0/limensafe-defaults.yaml
var defaultsYAML []byte
```

**Pros:**

- Embedded in binary
- No filesystem access needed

**Cons:**

- ❌ Doesn't work for user config discovery
- ❌ Loses reload capability (config is embedded)
- ❌ Complicates CDRL workflow (users would need to rebuild to change defaults)
- ❌ Not compatible with gofulmen/config's file-based approach

**Verdict:** Not compatible with requirements

## Decision

**We will implement Option 3**: Custom `findProjectRoot()` helper in `internal/config/loader.go`.

The function walks upward from the current working directory looking for project markers (`go.mod` or `.git`), then uses that as the base for constructing absolute paths to `config/` and `schemas/`.

### Implementation

```go
// internal/config/loader.go

// findProjectRoot walks up from the current working directory to find the project root.
// It looks for project markers like go.mod or .git directory.
// This ensures config paths work correctly regardless of where the process is run from.
//
// TODO: Consider refactoring to use gofulmen/pathfinder.FindRepositoryRoot() when available.
// This functionality would be a natural fit for pathfinder's upward traversal capabilities.
// See: internal coordination notes/gofulmen/config-path-resolution-issue.md for discussion.
func findProjectRoot() (string, error) {
    cwd, err := os.Getwd()
    if err != nil {
        return "", fmt.Errorf("failed to get current directory: %w", err)
    }

    current := cwd
    for i := 0; i < 10; i++ { // Search up to 10 levels
        // Check for project markers
        if fileExists(filepath.Join(current, "go.mod")) {
            return current, nil
        }
        if fileExists(filepath.Join(current, ".git")) {
            return current, nil
        }

        parent := filepath.Dir(current)
        if parent == current {
            break // Reached filesystem root
        }
        current = parent
    }

    return "", fmt.Errorf("project root not found (no go.mod or .git found)")
}

// In Load():
projectRoot, err := findProjectRoot()
if err != nil {
    return nil, fmt.Errorf("failed to find project root: %w", err)
}

catalog := schema.NewCatalog(filepath.Join(projectRoot, "schemas"))
opts := gfconfig.LayeredConfigOptions{
    Category:     "limensafe",
    Version:      "v1.0.0",
    DefaultsFile: "limensafe-defaults.yaml",
    SchemaID:     "limensafe/v1.0.0/config",
    Catalog:      catalog,
    DefaultsRoot: filepath.Join(projectRoot, "config"),
}
```

## Rationale

1. **Layer 2 Template Pattern**: As a Layer 2 template, we ship defaults locally (not from Crucible SSOT). We need reliable access to these local files.

2. **Test Reliability**: Go's test framework runs tests from the package directory. Without absolute paths, tests in `internal/config/` cannot find `../../config/` reliably.

3. **Developer Experience**: This approach "just works" from any directory - no special setup or environment variables required.

4. **Ecosystem Precedent**: This mirrors the pattern used in gofulmen's schema catalog (`resolveDefaultBaseDir()`) and is similar to how Go itself finds `go.mod`.

5. **Future Path**: We've documented (via TODO comment and memo) that this could be replaced with `gofulmen/pathfinder.FindRepositoryRoot()` when that function becomes available. This keeps the door open for standardization.

## Consequences

### Positive

- ✅ **Tests work from any directory**: `go test ./...` works as expected
- ✅ **Zero configuration**: Developers don't need to set environment variables
- ✅ **Idiomatic Go**: Follows common patterns (similar to `go.mod` discovery)
- ✅ **Easy replacement**: Can swap for library function when available
- ✅ **Documented intent**: TODO comment + memo explain this is temporary

### Negative

- ⚠️ **Performance overhead**: ~10 filesystem checks on every `config.Load()` (negligible in practice)
- ⚠️ **Unusual projects**: Could fail if project has neither `go.mod` nor `.git` (very rare)
- ⚠️ **Code duplication**: Each Layer 2 template will implement similar logic until gofulmen provides standard solution

### Mitigations

- **Performance**: Could cache the result if it becomes an issue (not needed yet - config loads in <10ms)
- **Unusual projects**: Could extend markers to include `.fulmen/app.yaml` or other indicators
- **Duplication**: Documented in memo to gofulmen team - may become `pathfinder.FindRepositoryRoot()` in future release

## Related Decisions

- Feature Brief 007: gofulmen/config Integration
- Layer 2 template pattern (consuming Crucible via gofulmen, not direct sync)
- Memo: `internal coordination notes/gofulmen/config-path-resolution-issue.md`

## References

- **Implementation**: `internal/config/loader.go:findProjectRoot()`
- **gofulmen Discussion**: `internal coordination notes/gofulmen/config-path-resolution-issue.md`
- **Pattern Precedent**: `gofulmen/config/layered.go:resolveConfigBaseDir()` (searches for `config/crucible-go`)
- **Pattern Precedent**: `gofulmen/schema/catalog.go:resolveDefaultBaseDir()` (searches for `schemas/crucible-go`)

## Notes

- **Search depth**: 10 levels chosen as reasonable limit (most projects are <5 levels deep)
- **Markers**: `go.mod` preferred over `.git` (checked first) as it's more definitive for Go projects
- **Filesystem root**: Properly detected via `filepath.Dir()` returning same path (cross-platform safe)
- **Error handling**: Returns descriptive error if project root not found (helps debugging)

---

**Status:** ✅ Accepted and Implemented (2025-11-15)
**Superseded by:** None (current)
**Revisit when:** gofulmen/pathfinder adds `FindRepositoryRoot()` function
