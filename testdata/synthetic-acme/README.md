# synthetic-acme — v0 Spike Corpus

Hand-crafted leak corpus using `acme` as the canonical synthetic
placeholder for a protected client identity. Recreates the datawidget
case shape without propagating real client vocabulary.

## Placeholder Convention

| Role                                  | Placeholder                   | Class                                            | Status                                                        |
| ------------------------------------- | ----------------------------- | ------------------------------------------------ | ------------------------------------------------------------- |
| Real client name (protected)          | `Acme` / `Acme Corp` / `acme` | `client_identity`                                | Blocked in public_oss/internal                                |
| Internal project codename (protected) | `horizon` / `project-horizon` | `codename`                                       | Blocked in public_oss                                         |
| Sanctioned substitute codename        | `tilden`                      | `codename` (with `replacement_for: client-acme`) | Allowed in engagement_private/internal; blocked in public_oss |
| Triangulation token (worst case)      | `acme-horizon-dev`            | (matched by co-occurrence rule)                  | Critical regardless of scope                                  |
| Short acronym regression token        | `ILT`                         | `operational_pattern`                            | Whole-word only; must not match inside ordinary words         |

## Layout

```text
synthetic-acme/
├── README.md (this file)
├── repo/                       # the "fake repo" pointed to by the scanner
│   ├── internal/
│   │   ├── cmd/profile_test.go        # acme-dev / acme-prod fixtures
│   │   ├── doctor/redact_test.go      # acme-horizon-dev triangulation
│   │   ├── config/loader_test.go      # acme-dev profile in config
│   │   ├── clients/acme/data.go       # path-only leak (T4 fixture)
│   │   └── locks/sha_noise.txt        # internal-brief short-acronym substring noise
│   └── docs/usage.md                  # prose mention of "Acme Corp"
├── git-fixtures/               # surfaces that aren't files
│   ├── branch-name.txt                # branch name with leak
│   └── commit-msg.txt                 # commit message with leak
├── catalog/
│   ├── synthetic-acme.catalog.yaml         # tier 2 (workspace-private)
│   └── synthetic-acme-public.catalog.yaml  # tier 1 (public, in-repo)
├── termlist/                   # `catalog build --from-termlist` worked example
│   ├── synthetic-acme.termlist.txt              # flat PROTECTED==>replacement input
│   └── synthetic-acme.from-termlist.catalog.yaml # golden generated catalog
├── .limensafe/
│   └── config.yaml             # repo config; declares public required + private optional
└── expected/                   # populated by v0-spike-plan.md
```

Catalog IDs follow the ID-safety rule (`catalog-schema.md`):
`cs-spike-private-v0` (tier 2) and `cs-spike-public-v0` (tier 1).
Entity IDs use opaque enumerated form: `e-client-1`, `e-codename-1`,
`e-codename-2`, `e-format-1`. Rule IDs: `r-cooccur-1`. None contain
protected alias substrings.

## Acceptance Properties (referenced from `problem-statement.md`)

When `limensafe scan corpus/synthetic-acme/repo --catalog
corpus/synthetic-acme/catalog/synthetic-acme.catalog.yaml --visibility
public_oss` runs:

1. **Findings non-empty.** Scanner detects all leak instances across
   `internal/`, `docs/`, branch name, commit message.
2. **Co-occurrence rule fires.** `acme-horizon-dev` produces a
   `severity: critical` finding from rule `acme-horizon-triangulation`,
   independent of the per-entity findings for `acme` and `horizon`.
3. **Zero-leak output.** No occurrence of `acme`, `horizon`, `tilden`,
   or any protected alias appears in:
   - `stdout` of the scanner
   - `stderr` of the scanner
   - any JSON output file
   - any log file written
     Verified by `grep -E "(acme|horizon|tilden)"` over all output streams
     returning **zero** matches.
4. **Replacement suggestions provided.** Each finding references a
   `replacement_id` (catalog-authored) so the user can map fix-ups
   without ever seeing the raw protected term in scanner output.
5. **Scope-adaptive severity.** Re-running with `--visibility
engagement_private` reduces severity for `tilden`-tagged matches
   (which are `allowed_in: [engagement_private]`) but does not change
   severity for `acme` or `horizon` (which are blocked in all
   non-`local_only` scopes).
6. **Whole-word short-acronym handling.** The `ILT` regression entity
   must not match inside common-substring noise such as `built`,
   `split`, `splittable`, `rebuilt`, `tilt`, or lockfile-style hashes.

## Why "acme" is safe

`acme` is the canonical synthetic placeholder in tech documentation,
RFC examples (e.g., RFC 8555 ACME protocol, `example.com`), test
fixtures, and tutorials. Using it as a fake-protected token in our
corpus does not create a CCL hazard.

For private testing against real client vocabulary, contributors should
build a separate, gitignored corpus (e.g., `corpus/engagement-foo/`)
referencing a private catalog bundle stored outside this repository.
