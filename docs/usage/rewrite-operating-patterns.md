# Rewrite operating patterns & the post-action hygiene contract

This page is for the operator who is about to rewrite git history to scrub
confidential vocabulary — or who is clearing a repo for publication after
someone else did. It answers two questions the tooling can't answer for you:

1. **Which rewrite did you actually run, and what did it leave untouched?**
2. **When is the rewrite _complete_** — not "did filter-repo exit 0", but
   "is the publishable surface actually clean"?

> **The one-sentence contract:** a rewrite is complete when **no artifact the
> rewrite produced — backup ref, archive tag, snapshot branch, in-tree
> term-list, in-tree callback script — remains on any surface that goes
> public**, and a `limensafe audit-publish` from a **fresh clone** of the
> publishable remote exits `0`.

`limensafe` detects and advises; it never deletes or rewrites refs. The
mechanics of the rewrite itself are `git filter-repo`'s job. This page is the
methodology that wraps both.

## The defeated-rewrite failure mode

The failure this contract exists to prevent (synthetic vocabulary —
`acme`/`horizon` stand in for real protected terms):

> A maintainer runs `git filter-repo --replace-text` to scrub `acme` and
> codename `horizon` from a repo's history. Before pushing, they create
> `backup/main-pre-rewrite-2026-05-14` on `origin` "for DR." The rewrite exits
> clean; every post-rewrite scan of `main` returns zero hits. Months later the
> repo flips to public. The backup branch — on `origin` the whole time —
> carries the **un-scrubbed** history verbatim. `acme` and `horizon` are one
> `git clone --mirror` away. **The rewrite's safety net defeated the rewrite.**

The logic is consistent and damning: the rewrite's _intent_ was "remove these
terms from the publish surface," but its _implementation_ moved them off the
primary line and onto a backup ref **on the same remote** — and the publish
surface is every ref on that remote. Net effect on what publishes: nothing
changed.

Two things make this more than a one-off mistake:

- **Team handoff.** The operator who runs the rewrite is rarely the one who
  clears the repo for publication. A teammate legitimately pulls the backup ref
  (it's on `origin`, so it's "real"), bases work on it, pushes it back — and now
  the scrubbed vocabulary lives on a fresh branch with no audit trail. A
  convention carried only in the original operator's memory **loses on every
  handoff.**
- **Verification aimed at the wrong unit.** Scanning `main` (even full history
  via `--git-history-all`) is structurally blind to other refs. The publish
  surface is the unit that matters; see
  [Audit before going public](../../README.md#audit-before-going-public-the-whole-publish-surface).

## Pick your rewrite mode deliberately

Different rewrites leave different things untouched. Picking a mode by accident
and verifying against a vague "is it clean now" is how residue survives. Choose
deliberately, write the choice down, and verify against **that mode's**
completeness shape.

| Mode                                             | Replaces                                                                            | Leaves untouched                                                      |
| ------------------------------------------------ | ----------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| **full-rewrite**                                 | every blob, every commit message, every author/committer identity, across every ref | nothing relevant to the publish surface                               |
| **content-only** (`--replace-text`)              | blob content                                                                        | commit messages, author/committer identity, tags pointing at old SHAs |
| **message-only** (`--message-callback`)          | commit messages                                                                     | blob content, identity                                                |
| **identity-only** (`--mailmap` / email callback) | author/committer identity                                                           | blob content, messages                                                |
| **tip-only** / squash-to-cutoff                  | discards all history before the cutoff                                              | provenance before the cutoff (and any term still present after it)    |

The trap: a `content-only` rewrite scrubs `acme` from blob _content_ but leaves
it in a **commit message** or an annotated **tag** — both publishable. If your
protected term ever appeared in a commit subject, a `--replace-text` run is not
enough, and a content scan of the working tree won't tell you. Match the mode to
where the term actually lived.

## The post-action hygiene contract

A rewrite is **complete** only when **all** of the following hold on the
**publishable** surface (the remote the repo publishes from):

1. **Targeted surfaces are clean.** The surfaces the chosen mode targets carry
   no residual catalog matches on any publishable ref.
2. **Every artifact the rewrite created is gone from the publishable remote:**
   - Backup branches — any ref matching `audit-publish`'s default danger
     patterns: `backup/*`, `*pre-rewrite*`, `*-snapshot-*`, `archive/*`,
     `*-bak`, `wip/*` (override with `--danger-pattern`).
   - Backup tags — same patterns.
   - The rewrite's term-list / replacement file, if it was committed.
   - The rewrite's callback scripts, if they were committed.
   - Any `.plans/`-style operator memo that got tracked by accident.
3. **A fresh-clone audit passes.** `limensafe audit-publish` run from a **fresh
   clone** of the publishable remote exits `0` (`summary.publish_safe: true`).
4. **The completion is recorded** in an operator-private incident note citing
   the chosen mode and the verification artifact.

### The contract is about the publishable surface — not absolute destruction

This is deliberately asymmetric. Keeping disaster-recovery copies of pre-rewrite
state is **encouraged** — they just must not live on the remote the repo
publishes from. Put them in operator-private storage, a different remote, or a
maintainer's local mirror. You do **not** have to choose between DR safety and a
clean publish surface; they live in different places.

## Verify from a fresh clone — and why

```bash
limensafe audit-publish --remote origin > publish-audit.json
jq '.summary.publish_safe' publish-audit.json
# false → for each ref in .summary.leak_vector_refs, follow its suggested_action,
#         then re-verify FROM A FRESH CLONE:
git clone <remote> /tmp/verify && cd /tmp/verify && \
  limensafe audit-publish --remote origin
```

The fresh clone is load-bearing. Auditing from the maintainer's working repo can
**mask** refs they pruned locally but never pushed the deletion of — the local
view says "gone," the remote still serves them, and the remote is what
publishes. A fresh clone sees exactly what the public would.

`audit-publish` flags backup-pattern refs as leak vectors **by name**, even when
their content scans clean and even when the objects aren't fetched — because a
ref whose name advertises a pre-action snapshot is, by contract, a leak vector
until it's off the publishable remote. `summary.publish_safe` cannot be `true`
while one exists. (Override the patterns for your org's conventions with
`--danger-pattern`.)

## See also

- [Audit before going public](../../README.md#audit-before-going-public-the-whole-publish-surface) — the `audit-publish` recipe.
- [ADR-0008: rewrite-completion contract](../decisions/ADR-0008-rewrite-completion-contract.md) — this contract as an architectural decision.
- [Scan modes & scopes](scan-modes.md) — the surface ladder (diff ⊂ ref-history ⊂ all-refs).
- [ADR-0003: redaction-safe output](../decisions/ADR-0003-redaction-safe-output.md) — the invariant a rewrite extends across history; this contract is what makes that extension durable.
