# Scan Modes & Scopes — which scan answers which question

`limensafe scan` can look at your repository from several different
angles, and they answer **different questions**. Picking the wrong scope
is the most common source of "why is this so noisy?" frustration: a scan
meant to gate a single change should not re-report everything already in
the tree.

Use this table to jump to the right section.

| Your situation                            | Question you're really asking                     | Go to                                                                       |
| ----------------------------------------- | ------------------------------------------------- | --------------------------------------------------------------------------- |
| About to commit; want a fast safety check | "Will _this commit_ leak something?"              | [Pre-commit gate](#pre-commit-gate-staged-index)                            |
| Reviewing a branch / PR before merge      | "Did _this change_ introduce a leak?"             | [Per-change gate](#per-change-gate-the-diff-question)                       |
| Want to check the repo as it stands today | "Is anything currently in the tracked tree?"      | [Tracked-tree scan](#tracked-tree-scan-everything-published-at-head)        |
| Checking a specific file or directory     | "Is this path clean?"                             | [Path scan](#path-scan-a-file-or-directory)                                 |
| Writing a commit message or branch name   | "Does this Git metadata leak?"                    | [Git-metadata surfaces](#git-metadata-surfaces-commit-message--branch-name) |
| Auditing before a history rewrite         | "Was anything _ever_ committed, even if deleted?" | [Full-history audit](#full-history-audit)                                   |
| Deciding what to wire into CI             | "Where do I put the blocking check vs the audit?" | [Gate vs sweep](#gate-vs-sweep-the-recommended-setup)                       |

> **Mental model: four scopes, narrow to broad.** _diff lines_ ⊂
> _changed files_ ⊂ _working tree_ ⊂ _full history_. The narrower the
> scope, the quieter and more blockable the scan; the broader the scope,
> the more it answers "is there anything else?" — at the cost of noise
> you must triage. Match the scope to the question.

All examples below use the in-repo public-baseline catalog so they run
as-is. In real use you point `--catalog` at a catalog file you control
(see [Catalog & visibility](#catalog--visibility)). Synthetic names
`acme` (client) and `horizon` (codename) stand in for protected
vocabulary throughout.

---

## Pre-commit gate (staged index)

**Question:** "Will the change I'm about to commit leak something?"

Scans the files in the Git staging index (added or modified). This is
the soundest fast gate for a pre-commit hook — it sees exactly what
you're about to record.

```bash
limensafe scan --staged \
  --catalog pkg/catalog/builtin/public-baseline.yaml \
  --visibility public_oss
```

Exit 0 = clean, exit 1 = a finding crossed the block threshold. Wire it
into `.git/hooks/pre-commit` or your pre-commit framework and let exit 1
stop the commit.

**Caveat:** `--staged` scans the _full content_ of staged files, not
just the lines you changed. A pre-existing match in a file you happen to
be editing will surface. For "only what this change introduces," see the
per-change gate below.

---

## Per-change gate (the "diff" question)

**Question:** "Did _this branch / PR_ introduce a leak, independent of
what was already there?"

This is the quietest, most blockable scope — it fires only on content a
change adds relative to a base ref. It is the natural engine for a PR CI
check and for a meaningful "scan ran and passed" attestation.

Use `--diff` with an explicit base ref:

```bash
limensafe scan . --diff --diff-base origin/main \
  --catalog "$YOUR_CATALOG" --visibility public_oss
```

The diff surface parses `git diff --unified=0 <base>...HEAD` and scans
only post-image added hunks. Pre-existing matches in touched files do not
fire. Findings emit `source_kind: "git_diff"` and
`location.surface_kind: "diff"`.

---

## Tracked-tree scan (everything published at HEAD)

**Question:** "Is anything in the repository _as it stands today_?"

Scan the tracked tree — every file Git would publish — without dragging
in `.git/`, build caches, or local-only scratch directories. The native
way to express "tracked files only" is `--git-archive <ref>`:

```bash
limensafe scan --git-archive HEAD \
  --catalog "$YOUR_CATALOG" --visibility public_oss
```

You can also scan another ref without checking it out:

```bash
limensafe scan --git-archive origin/main \
  --catalog "$YOUR_CATALOG" --visibility public_oss
```

The output reports `scan_metadata.scan_root` as the ref (`HEAD`,
`origin/main`, or a SHA), with `scan_root_kind: "git-archive"` and
`git_ref`. Temporary extraction paths are not part of stdout/stderr.

The equivalent manual recipe is still useful for understanding the
composition:

```bash
work=$(mktemp -d)
git archive HEAD | tar -x -C "$work"
limensafe scan "$work" \
  --catalog "$YOUR_CATALOG" --visibility public_oss
rm -rf "$work"
```

Why the archive instead of `limensafe scan .`? `git archive HEAD` gives
you exactly the tracked, publishable surface. Plain directory scans now
honor root-level `.gitignore` and `.limensafeignore` files by default,
which is useful for dev loops, but git archives remain the crisp CI
shape.

**Expect some noise at this scope.** A tracked-tree scan answers "is
there anything else?" and will surface pre-existing matches that aren't
leaks — generic domain words, or short catalog acronyms matching inside
unrelated strings (e.g. dependency hashes). That noise is acceptable for
an _audit_ but is why you don't gate commits on this scope. Triage it;
don't block on it.

---

## Path scan (a file or directory)

**Question:** "Is this specific file or directory clean?"

```bash
limensafe scan path/to/dir \
  --catalog "$YOUR_CATALOG" --visibility public_oss

limensafe scan path/to/one-file.md \
  --catalog "$YOUR_CATALOG" --visibility public_oss
```

The positional path may be a file or a directory (directories scan
recursively). Useful for spot checks and for scanning generated output
before you publish it.

---

## Git-metadata surfaces (commit message & branch name)

**Question:** "Does this commit message or branch name leak?"

These surfaces read from stdin, with `-` as the path argument:

```bash
# Commit message
echo "$msg" | limensafe scan - --commit-msg \
  --catalog "$YOUR_CATALOG" --visibility public_oss

# Branch name
echo "feature/horizon-rollout" | limensafe scan - --branch-name \
  --catalog "$YOUR_CATALOG" --visibility public_oss
```

Handy in a `commit-msg` or `pre-push` hook: branch names and commit
subjects are a real leak surface (they end up in `git log` and on the
remote) and are easy to forget.

---

## Full-history audit

**Question:** "Was anything _ever_ committed — including in blobs that
are no longer in the tree, or in old commit messages?"

This is the pre-rewrite-remediation question: before running
`git filter-repo`, you want every historical instance of a protected
term, not just what HEAD shows.

Scan historical blobs:

```bash
limensafe scan . --git-history \
  --catalog "$YOUR_CATALOG" --visibility public_oss
```

Scan historical commit messages:

```bash
limensafe scan . --git-commit-messages \
  --catalog "$YOUR_CATALOG" --visibility public_oss
```

Typical pre-rewrite audit: scan both surfaces in one run:

```bash
limensafe scan . --git-history-all \
  --catalog "$YOUR_CATALOG" --visibility public_oss
```

History mode enumerates commits reachable from all refs, scans each
unique blob once, and expands content findings to the commit/path
attributions that reference that blob. Findings use
`source_kind: "git_history_blob"` with `location.surface_kind: "blob"`
for historical blobs, and `source_kind: "git_commit_message"` with
`location.surface_kind: "commit_message"` for historical commit
messages. `location.git_ref` is the commit SHA. Run metadata reports
`scan_root_kind: "git-history"`, `git_ref: "--all"`, and
`history_blobs_scanned`, `history_commits_scanned`, and
`history_unique_blobs`.

Historical path-segment findings are generated per commit/path
attribution. Blob-content co-occurrence rules stay scoped to one blob's
content; historical path findings do not co-occur with content findings.

History scanning stresses the [zero-leak invariant](../decisions/ADR-0003-redaction-safe-output.md)
hardest: a real finding means the protected term is already in `.git`, so
every byte the tool reports must still be redaction-safe.

---

## Catalog & visibility

Every scan needs a **catalog** (the vocabulary to look for) and a
**visibility** (the disclosure scope to judge against).

- `--catalog <file>` — repeatable; layers multiple catalog files. Point
  it at a catalog you control. The vendored
  `pkg/catalog/builtin/public-baseline.yaml` covers generic sentinel
  markers only — never organization-specific vocabulary. See the
  [catalog schema](../design/catalog-schema.md) for authoring.
- `--visibility <scope>` — one of `public_oss`, `unlisted_oss`,
  `internal`, `engagement_private`, `local_only`. An entity's
  `blocked_in` set is judged against this. Scanning at the _most
  restrictive_ visibility your content will reach (usually `public_oss`
  for anything bound for a public release) is the safe default.
- `--config-file <.limensafe/config.yaml>` — resolves catalogs by
  reference and supplies the repo's visibility, so CI invocations don't
  repeat flags.
- `--mode <local|ci|release>` — applies posture defaults. `local`
  warns when optional private catalogs are missing; `ci` and `release`
  fail closed. `release` also tightens the block threshold to `medium`.
- `--private-catalog-missing <silent|warn|error>` — explicit override
  for missing optional private catalogs. This flag wins over both config
  and `--mode`.

Protected vocabulary lives in catalogs **outside** the repo; only
synthetic placeholders and the public baseline ship inside it.

---

## Exit codes

The exit-code contract is locked — CI wrappers and hooks depend on it:

| Code | Meaning                                                    |
| ---- | ---------------------------------------------------------- |
| `0`  | Scan succeeded; no finding at or above the block threshold |
| `1`  | Scan succeeded; one or more findings have `decision=block` |
| `2`  | Config / catalog validation error                          |
| `3`  | Runtime error (I/O, malformed input)                       |

stdout is always JSON; diagnostics go to stderr. Don't parse stderr for
results.

---

## Decision model: severity → block vs warn

Every finding carries a **severity**; whether it **blocks** (exit `1`) or
merely **warns** (advisory, exit `0`) comes down to one comparison
against the **block threshold**:

> A finding blocks when its severity is **at or above** the block
> threshold. Otherwise it warns.

Severities rank `info` < `low` < `medium` < `high` < `critical`. The
threshold defaults to `high`, and `--mode` adjusts it:

| `--mode`           | Block threshold | Missing optional private catalog | Use for                  |
| ------------------ | --------------- | -------------------------------- | ------------------------ |
| _(none)_ / `local` | `high`          | warns                            | dev loop                 |
| `ci`               | `high`          | **fails closed** (exit `2`)      | PR gates, merge checks   |
| `release`          | `medium`        | **fails closed** (exit `2`)      | release prep — strictest |

So a `medium`-severity finding is a **warn** under `local`/`ci` but a
**block** under `release`: release posture deliberately treats
medium-confidence noise as a stop, because shipping a leak in a _tagged_
artifact costs more than a false stop during release prep. You can also
set `block_threshold` directly under `policy:` in
`.limensafe/config.yaml` for a fixed threshold independent of mode.

Two **entity-level** controls decide whether a finding is produced at
all, before the threshold is even consulted:

- **`allowed_in: [<visibility>]`** — if the entity is allowed at the
  visibility you're scanning, it produces **no finding** there (a
  sanctioned codename inside an `engagement_private` repo, say); at a more
  restrictive visibility it fires normally.
- **`severity_override`** — sets the severity that feeds the comparison
  above.

This is why the same content can **block** at `--visibility public_oss
--mode release` yet stay **silent** at `--visibility engagement_private
--mode local`: one catalog, two different disclosure questions.

---

## Gate vs sweep (the recommended setup)

The single most useful habit is to run **two checks with different
scopes and different severities**:

|              | Gate                                        | Sweep                                    |
| ------------ | ------------------------------------------- | ---------------------------------------- |
| Scope        | per-change / staged index                   | tracked tree (and history at milestones) |
| When         | every commit + every PR                     | on a cadence — release prep, weekly cron |
| Blocking?    | **yes** — exit 1 stops the merge            | **no** — advisory triage list            |
| Noise budget | near-zero (must stay quiet to stay trusted) | tolerated (you triage it)                |
| Answers      | "did this change leak?"                     | "is there anything else?"                |

Why split them: the noise that's _acceptable_ in a sweep (a domain word,
a dependency-hash substring match) is _fatal_ in a gate — developers who
hit false blocks learn to bypass the hook, and then the gate protects
nothing. Keep the gate quiet and blockable; let the sweep be thorough and
advisory.

For the concrete CI wiring of this pattern — GitHub Actions jobs,
sourcing the catalog from a secret without committing it, and exit-code
handling — see [Integrating limensafe into CI/CD](ci-integration.md).

---

## See also

- [Integrating limensafe into CI/CD](ci-integration.md) — wire the gate
  and sweep into a pipeline
- [ADR-0003 — redaction-safe output](../decisions/ADR-0003-redaction-safe-output.md)
  (the zero-leak invariant every scan respects)
- [Catalog & config schema](../design/catalog-schema.md)
- [Roadmap](../roadmap.md) — where the remaining post-MVP scan surfaces
  and hardening items sit
