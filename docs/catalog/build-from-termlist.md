# Building a catalog from a term-list

`limensafe catalog build` turns a **term-list** or small structured corpus
input into a schema-conformant catalog YAML. The original
`PROTECTED==>replacement` mapping grammar remains unchanged; structured
`regex:` and `allowlist:` lines add the cases that a flat literal list cannot
express without hand-authoring YAML.

> **The term-list is sensitive.** A real term-list names your clients,
> codenames, or people — exactly the vocabulary limensafe exists to keep
> out of the open. It lives **outside the repository tree**, same as the
> catalog it produces (the two-layer catalog rule). The only term-list in
> this repo is the synthetic worked example under
> `testdata/synthetic-acme/termlist/`.

## Literal mappings

```text
# Comments start with '#'. Blank lines are ignored.
PROTECTED==>replacement
PROTECTED==>replacement   # class=client_identity severity=high
```

- **`PROTECTED`** is the term to detect; it becomes an entity **alias**.
- **`replacement`** is what to use instead; it becomes the entity's
  `replacement_suggestion`.
- Terms that **share a replacement collapse into one entity**. List every
  surface form of the same thing pointing at the same replacement:

  ```text
  AcmeCorp==>ClientAlpha
  Acme==>ClientAlpha
  ```

  → one entity, two aliases, one `replacement_suggestion: ClientAlpha`.

### Per-line directives

A trailing ` # class=… severity=…` directive overrides the class and
severity for a replacement group:

```text
AcmeCorp==>ClientAlpha   # class=client_identity severity=high
Acme==>ClientAlpha
```

The directive applies to the **whole group**, so putting it on the first
line of a group is enough — undirected sibling lines inherit it. Two lines
in the same group that **both** specify a class (or severity) and
**disagree** are a line-numbered error, so a typo can't silently split a
group's policy.

- **`class`** — the entity class. Defaults to `codename`
  (`--default-class` changes the floor). **Prefer a specific class**
  (`client_identity`, `person`, …) for anything you know; the default is a
  low-floor catch-all, not a recommendation.
- **`severity`** — the per-entity `severity_override`. Omit it to fall
  back to the catalog-level `--default-severity`.

Unknown directive keys are rejected (with a value-free, line-numbered
error) so a misspelled key surfaces instead of being silently dropped.

## Structured lines

Structured lines are additive. Existing literal-only inputs keep producing
the same generated YAML.

```text
regex:\bHRZN-[0-9]{4}\b   # class=operational_pattern severity=high
allowlist:literal:HRZN-0000   # case_insensitive=true whole_word=true
allowlist:regex:\bHRZN-9[0-9]{3}\b   # case_insensitive=true whole_word=true
```

- **`regex:`** emits one entity with a `regex_patterns` entry. The pattern is
  validated at build time; invalid regexes fail with line-number-only
  diagnostics and no output file.
- **`allowlist:literal:`** and **`allowlist:regex:`** emit top-level internal-brief
  allowlist entries. They author suppression entries only; the suppression
  semantics are the same as hand-authored catalog allowlists.
- Regex and allowlist IDs are stable, opaque, hash-derived values. They never
  contain the raw pattern.
- `class` and `severity` apply to literal mapping groups and `regex:` lines.
  `case_insensitive` and `whole_word` apply to `allowlist:` lines.

## Running it

```bash
limensafe catalog build \
  --from-termlist /secure/out-of-tree/terms.txt \
  --out          /secure/out-of-tree/my.catalog.yaml \
  --catalog-id   my-org-private \
  --default-severity medium
```

| Flag                 | Required | Notes                                                                    |
| -------------------- | -------- | ------------------------------------------------------------------------ |
| `--from-termlist`    | yes      | Path to the term-list / structured corpus input, or `-` for stdin.       |
| `--out`              | yes      | Where to write the catalog. **No stdout default** — output is sensitive. |
| `--catalog-id`       | yes      | `catalog_id` for the generated catalog.                                  |
| `--default-class`    | no       | Class for entities without a directive (default `codename`).             |
| `--default-severity` | no       | Catalog-level `default_severity`.                                        |
| `--no-whole-word`    | no       | Disable whole-word matching (it is **on** by default).                   |

### Defaults that match the validated posture

Generated entities use the **catalog-B** posture from the release-gate
dogfoods: `case_insensitive` + `slug` + `whole_word`, all on. Whole-word
matching is on by default because it removes the substring false-positive
class (e.g. a short token matching inside `built`/`split`); pass
`--no-whole-word` only when you deliberately want substring matching.

### Stable, opaque entity ids

Each literal entity gets a hash-derived id (`e-tl-<hex>`) computed from its
replacement group. Regex entities use `e-rx-<hex>`, and generated allowlist
entries use `al-tl-<hex>`. Ids are **never positional** and never contain raw
protected text or regex patterns, so editing the input (adding, removing,
reordering lines) never renumbers unrelated entries. Output is
**deterministic** — rebuilding the same input produces a byte-identical
catalog.

## Safety properties

- **`--out` is required; there is no stdout default.** The generated
  catalog _is_ protected vocabulary; it should never land in a shell
  history, a CI log, or a terminal scrollback by accident.
- **Diagnostics are redaction-safe.** A malformed line is reported by line
  number and structural reason only — never by echoing the protected term,
  replacement, regex pattern, allowlist pattern, directive value, or raw line.
  This holds on every error path (ADR-0003).
- **Output is validated before it's written.** The generated bytes are
  round-tripped through the catalog loader and JSON Schema; an invalid
  result fails the build rather than writing a broken catalog.

## Exit codes

| Code | Meaning                                             |
| ---- | --------------------------------------------------- |
| `0`  | Catalog generated and written.                      |
| `2`  | Invalid term-list or options (validation error).    |
| `3`  | Runtime error — input read or output write failure. |

These match the `scan` CLI contract, so the command composes in the same
pipelines and pre-commit hooks.

## Worked example

The synthetic worked examples live at
`testdata/synthetic-acme/termlist/`.

Literal-only:

```bash
limensafe catalog build \
  --from-termlist testdata/synthetic-acme/termlist/synthetic-acme.termlist.txt \
  --out          /tmp/synthetic.catalog.yaml \
  --catalog-id   cs-synthetic-acme-from-termlist \
  --default-severity medium
```

The committed `synthetic-acme.from-termlist.catalog.yaml` is the generated
output (carrying repo formatting) and is regression-checked against the
builder, so it doubles as a golden reference for the generated structure.

Structured corpus:

```bash
limensafe catalog build \
  --from-termlist testdata/synthetic-acme/termlist/synthetic-acme.structured-corpus.txt \
  --out          /tmp/synthetic-structured.catalog.yaml \
  --catalog-id   cs-structured-corpus-demo \
  --default-severity medium
```

The structured fixture is built during tests and round-tripped through the
catalog loader; no hand-authored YAML is required for the regex/allowlist
case.

## See also

- [Sourcing the catalog in CI](../usage/ci-integration.md#building-a-catalog-from-a-term-list-in-ci) — building from a term-list secret ephemerally.
- [ADR-0006: catalog two-layer validation](../decisions/ADR-0006-catalog-two-layer-validation.md) — the schema the generated catalog conforms to.
- [ADR-0003: redaction-safe output](../decisions/ADR-0003-redaction-safe-output.md) — the zero-leak invariant the diagnostics uphold.
