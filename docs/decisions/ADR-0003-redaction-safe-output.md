# ADR-0003: Redaction-Safe Output Contract for V0 Spike

Status: Proposed
Date: 2026-04-29
Owner: cxotech (drafted while entarch reviews v0-spike-plan)

## Context

The zero-leak invariant — "no catalog-protected substring appears in any
output stream under default settings" — is the single most spike-critical
property of limensafe. It is also the property most easily violated by
ad-hoc developer behavior:

- A `fmt.Println` for debugging that echoes a raw match
- A `log.Printf("error processing %s", path)` where `path` contains an
  alias as a path segment
- An `errors.Wrap(err, fmt.Sprintf("scanning %s", branch))` where
  `branch` is the protected branch-name surface
- A panic stack with file paths embedded
- A SARIF-style snippet field that includes context lines

Each of these is plausible during build. Without an implementation
discipline that makes the safe path the easy path, the invariant erodes.

Per entarch's amendment locked 2026-04-29: the invariant covers **all**
output text, including paths, surface labels, catalog source paths, and
output-visible IDs. The output must remain *actionable* (file path + line
number) without echoing any protected substring.

## Decision

### 1. Single output formatter is the only allowed serialization path

All output to `stdout`, `stderr`, log files, JSON files, and any other
sink goes through one package: `pkg/output/`. The package exports:

```go
// All emission goes through these. Direct fmt.Println / log.Printf
// in user-facing paths is a CI lint failure.
type Formatter interface {
    EmitFinding(f Finding) error
    EmitSummary(s ScanSummary) error
    EmitMetadata(m ScanMetadata) error
    EmitWarning(code WarningCode, args ...any) error
    EmitError(code ErrorCode, args ...any) error
    Close() error
}

// Constructors
func NewJSONFormatter(w io.Writer, redactor *Redactor) Formatter
func NewHumanFormatter(w io.Writer, redactor *Redactor) Formatter
```

Internal helpers (`fmt.Println`, `log.*`) for non-user-visible paths
(developer debug builds with `-tags debug`) are allowed but stripped at
release-build time and not exercised by the CI grep test.

### 2. Every emitted string passes through `redactStr()` before write

```go
type Redactor struct {
    aliasMatcher *ahocorasick.Matcher  // built from loaded catalog aliases
    entityByAliasIndex map[int]*Entity // map matcher state -> entity
}

// Replaces any alias substring with an opaque marker.
// `<r:<entity_id>>` for matched aliases; passthrough otherwise.
// IDs are guaranteed safe by the ID-safety rule (catalog-schema.md).
func (r *Redactor) Redact(s string) string { ... }
```

The Aho-Corasick automaton is built once at scan start from all loaded
catalogs and reused for every emission. Performance overhead is
sub-linear in alias-set size; for typical catalogs (10-100 entities,
each with a handful of aliases) the overhead is negligible relative to
filesystem traversal.

### 3. Replacement marker form

For an alias matching entity `e-client-1`:

```text
input:  "scanning profile acme-dev"
output: "scanning profile <r:e-client-1>-dev"
```

Properties:
- The marker `<r:e-client-1>` references the entity ID, which is
  guaranteed alias-free by the ID-safety rule
- A reader can map markers back to entities via the catalog (under
  `--verbose` or in a separate explanation tool)
- The marker is short, machine-greppable, and visually distinct from
  normal text
- Path segments are redacted segment-by-segment:
  `internal/clients/acme/data.go` → `internal/clients/<r:e-client-1>/data.go`

Alternative considered: full HMAC fingerprint replacement
(`<hmac:abc123…>`). Rejected for v0 because (a) markers are easier to
debug than opaque hashes, and (b) the entity ID is already a stable
reference that survives across runs.

### 4. Template-based message construction (no Sprintf with raw values)

```go
// FORBIDDEN
msg := fmt.Sprintf("Detected %s at %s:%d", finding.Evidence, finding.Path, finding.Line)

// REQUIRED — template uses safe IDs and structured fields, never raw values
msg := output.Render(MsgFindingDetected, struct {
    EntityID   string
    EntityClass string
    Severity   string
    PathRedacted string
    Line       int
    Column     int
}{
    EntityID:    finding.EntityID,
    EntityClass: finding.EntityClass,
    Severity:    finding.Severity.String(),
    PathRedacted: redactor.Redact(finding.Path),
    Line:        finding.Line,
    Column:      finding.Column,
})
```

The template registry (`pkg/output/templates.go`) is a fixed,
auditable set. Adding a new template requires a code change and a
review against the invariant. Templates may reference IDs, classes,
severities, line numbers, and pre-redacted strings — never raw values.

### 5. Surface labels are enums, not strings

```go
type Surface int
const (
    SurfaceContent Surface = iota
    SurfacePath
    SurfaceBranchName
    SurfaceCommitMessage
    SurfaceMetadata
)

// JSON field becomes "surface": "branch_name"
// Never "surface_value": "feat/acme-redash-fix"
```

Findings on `branch_name` or `commit_message` surfaces emit only the
enum identifying the surface plus location coordinates. The raw branch
or message text is consumed by the engine and discarded after detection.

### 6. CI grep test as second-line defense

The limensafe repo's CI runs the v0 acceptance suite, then verifies:

```bash
LEAK_PATTERN='(?i)(acme|horizon|tilden|Acme|Horizon|Tilden)'
ALL_OUTPUT="$(cat /tmp/scan.json /tmp/scan.txt /tmp/scan.err /tmp/scan.log)"
if echo "$ALL_OUTPUT" | grep -qE "$LEAK_PATTERN"; then
    echo "FAIL: redaction-safe invariant violated"
    exit 1
fi
```

This catches:
- A new `fmt.Println` slipped past code review
- A library upstream that logs to stderr unexpectedly
- A formatter template that referenced a raw field
- A panic with raw context

The grep test is **not** the primary enforcement mechanism — it's the
safety net. The single-formatter discipline (decision 1) is the
primary mechanism.

### 7. Verbose vs reveal: two distinct flags

`--verbose` and the proposed `--unsafe-reveal` flag serve different
purposes and must not be conflated.

**`--verbose`** — observability without breaking the invariant:
- Surfaces additional structured fields (detector internals, fingerprints, catalog metadata, severity composition trace)
- Every emitted string still passes through `redactor.Redact()`
- Safe in CI; safe in workflow logs; safe in agent contexts
- Default for build/diagnostic shell pipelines

**`--unsafe-reveal`** — explicit invariant suspension:
- Disables the redactor entirely (raw matched values appear in output)
- Required to be explicitly passed AND requires env gate `LIMENSAFE_UNSAFE_REVEAL=1` to function (belt-and-suspenders against accidental scripted use)
- Logs a warning to stderr on every invocation
- Audit-traced: emits a finding-class event noting the invariant suspension to the run's metadata
- **Disabled in CI by policy.** `pkg/output/` checks `os.Getenv("CI")` and refuses to honor `--unsafe-reveal` when set. CI workflows that need to bypass for forensic reasons must explicitly unset `CI` and re-set the unsafe-reveal env var, leaving an audit trail
- Intended for one-off local debugging in a private terminal where the operator already has the catalog loaded

In short: `--verbose` is for "more data, still safe"; `--unsafe-reveal` is for "raw values, accept the risk."

### 8. Logging follows the same contract

All `log.*` calls in production paths route through a logger that wraps
each formatted string in `redactor.Redact()`. The logger is constructed
with the same `Redactor` instance used by the output formatter.

`pkg/logging/` (likely a thin wrap around `gofulmen/logging`) exposes:

```go
type Logger interface {
    Info(code WarningCode, args ...any)
    Warn(code WarningCode, args ...any)
    Error(code ErrorCode, err error, args ...any)
}
```

All structured arguments pass through `redactor.Redact()` before
formatting; raw `error` text is also redacted. Underlying `slog`/`zerolog`
may be used; the contract is the wrapper.

## Consequences

### Positive

- One chokepoint enforces the invariant — easier to audit, easier to
  test, easier to teach
- CI grep test catches regressions automatically
- Developer guidance is simple: "use the formatter; never `fmt.Println`
  in production paths"
- Structured findings remain debuggable via `--verbose` mode that surfaces
  catalog metadata without leaking raw values
- Log files in CI are safe to upload as workflow artifacts (no
  post-process redaction needed)

### Negative

- Adding a new emission path requires touching the formatter
  (intentional friction)
- Performance overhead of the redactor pass on every string. Mitigated
  by Aho-Corasick (linear time in input length); negligible vs. file IO
- Debugging is slightly harder when a value is redacted but the
  developer wants to see the raw. **Default `--verbose` mode does NOT
  break the invariant** — it surfaces additional structured fields
  (catalog metadata, detector internals, span coordinates, finding
  fingerprints) but every emitted string still passes through the
  redactor. A separate `--unsafe-reveal` flag is the only way to
  disable redaction; it is gated, scary, and disabled in CI by
  policy. See "Verbose vs reveal" below.
- The marker form `<r:e-client-1>` is opinionated; some users may
  prefer different rendering. v1 can add a `--redaction-marker-format`
  flag if demand exists

### Testing strategy

Three tiers:

1. **Unit tests** in `pkg/output/`: feed strings containing every alias
   in a fixture catalog through the redactor; assert output contains
   none of them
2. **Integration tests** against `corpus/synthetic-acme/`: run the full
   `limensafe scan` and grep the output streams for protected
   substrings; expect zero hits (T3-T6 from `v0-spike-plan.md`)
3. **CI gate**: the grep test runs on every PR to the limensafe repo;
   blocking

## Open Questions

- Should the marker form include severity or class? My lean: no — IDs
  alone are sufficient; class/severity are already in structured
  fields.
- Should redaction apply to JSON field *values* only, or also to keys?
  My lean: values only — keys are catalog-author-controlled (`message`,
  `replacement_id`) and the ID-safety rule already prevents leaks in
  keys. But this needs a test fixture to verify.
- Does redaction apply inside structured fields like `expected/`
  fixtures generated for tests? My lean: yes — fixtures are output
  artifacts and should follow the same rule. The `expected/` files
  in `corpus/synthetic-acme/` will be redaction-safe by construction.

## Cross-References

- `v0-spike-plan.md` § 3 — JSON output contract
- `problem-statement.md` § Success Criteria #1 — zero-leak invariant
- `catalog-schema.md` § ID Safety Rule — why IDs are alias-free
- `architecture.md` § Finding Model — the structured fields the
  formatter consumes
