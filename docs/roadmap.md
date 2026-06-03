# Roadmap — limensafe

Forward-looking plan for limensafe. Reads from concrete (next release)
to directional (next major). Updated at major milestones; not a
contract. Historical detail of what shipped lives in
[`../CHANGELOG.md`](../CHANGELOG.md). The standing ops channel for
roadmap discussion is `the internal coordination channel`.

## v0.0.3 — Pre-handoff shakeout (DONE)

**Ready to tag**: 2026-05-16 (gofulmen v0.3.5 pre-tag gate cleared)
**Owner**: cxotech + entarch (org-spanning)

The v0.0.3 cycle's purpose was to give the incoming the maintainer team a
**clean, fully-shipped reference template**: CICD pipeline, release
signing, 5-platform build matrix, locked CLI contract, full handoff
documentation. Triggered by the partner-integration (DataWidget) integration
beta-test that surfaced an identity-shadow bug and concrete CI-contract
feedback from devlead.

Slices delivered:

1. Identity-shadow fix — local workaround during the cycle; gofulmen
   v0.3.5 shipped the precedence reorder upstream 2026-05-12;
   workaround removed and pin bumped 2026-05-16
2. CICD: ci.yml + release.yml on goneat-tools-runner v0.3.3,
   bootstrap-smoke job per india's 5-check spec, CGO=0, GOPATH prep
3. Release signing hardening: minisign required, TTY guard fix for CI,
   `make release-verify-signatures` as upload-chain gate
4. 5-platform release matrix: linux/{amd64,arm64} + darwin/{amd64,arm64}
   \+ windows/{amd64,arm64} (6 binaries actually)
5. Exit-code 0/1/2/3 contract + stream separation lock, locked by
   integration tests
6. Handoff doc slate: HANDOFF.md, CONTRIBUTING.md, MAINTAINERS.md
   (Dave + the maintainer team), CHANGELOG.md seed, this roadmap
7. Productbook entry formalized at
   `the internal productbook/content/projmgmt/limensafe/`

See [`../CHANGELOG.md`](../CHANGELOG.md#v003--2026-05-16-ready-to-tag) for the detailed shipping notes.

## v0.0.4 — the maintainer team first cycle (FIRM)

**Target**: Q3 2026 (the maintainer team paces; first release after handoff +
walkthrough)
**Owner**: devlead + devrev (with cxotech on
standby for v0 context)

**Theme**: Make the dogfood-and-dev-loop story tractable. Everything
in v0.0.4 is concrete and either has design context in this repo's
existing docs/decisions/ or carries clear hand-off context from partner-integration.

### Firm scope

- **`.limensafeignore` + `.gitignore`-aware skip** for working-tree
  dev-loop scans. Without this, scanning a project that contains
  limensafe's own corpora (or any repo that contains its own catalogs)
  blocks on the catalog/fixture files. Catalog: scan filters before
  the extractor walks; mirror gitignore semantics.
- **Dogfood scan** — `make dogfood-scan` + dedicated CI job for
  limensafe scanning its own repo. Depends on `.limensafeignore`
  landing first (so catalog/, testdata/, README example patterns can
  be excluded). Wired into CI before tag for v0.0.4 itself.
- **`--git-archive HEAD` convenience flag** — landed in the v0.0.5
  wave. It replaces the temp-dir + mktemp + trap dance in user Makefiles
  while preserving the tracked-archive recipe's semantics.
- **`policy.block_threshold` from repo config** — landed in the
  v0.0.5 wave. Projects can set medium as the gate, and
  `--mode release` applies that tighter threshold.
- **Workhorse-template HTTP server cleanup** — `internal/server/*`
  is groningen-template inheritance that limensafe doesn't expose via
  CLI (no `serve` subcommand wires it). The HTTP server's flaky
  metrics integration test should either be removed (if HTTP isn't
  limensafe's roadmap) or properly hooked into a CLI surface.
- **Mode-aware missing-private-config** (partner-integration input from india-
  devlead) — landed in the v0.0.5 wave. `--mode local` warns,
  `--mode ci` / `--mode release` fail closed, and
  `--private-catalog-missing` provides an explicit override.
- **`limensafe profile doctor`** (partner-integration input) — `profile` source
  kind already recognized as v0.x-scope; this slice implements it
  along with a `doctor` subcommand that verifies private catalog
  presence, schema validity, and redaction-safe loading without
  printing protected vocabulary.

### Likely also v0.0.4 (firm but lower urgency)

- **gofulmen v0.4.x adoption** — pick up whatever Lima ships post-
  v0.3.5. The separate layered-config-defaults bug india flagged is
  on Lima's queue; pin to the version that closes it once it ships.

## v0.0.5 — TBD (DIRECTIONAL)

**Target**: late 2026 (the maintainer team paces)

Mostly placeholder. Likely candidates depending on adoption signals:

- **Performance** — `make perf-smoke` baselines on tracked
  large-corpus targets (Hugo, kubernetes source mirror). Profile
  hotspots in the engine + extractor. Today's 222ms on Hugo is a
  starting point, not a ceiling.
- **Multi-arch container image** — pair with the
  `ubuntu-latest-arm64-s` native runner pattern for QEMU-free
  manifest-merge builds (see fulmenhq org convention). Useful if
  limensafe ships in CI runner images.
- **Findings de-duplication** — same alias matching at multiple
  surfaces (file + commit-msg + branch) currently surfaces three
  findings. Provide a dedup mode for hooks where this is noisy.

## v0.1.0 — NER + extractor expansion + public release (DIRECTIONAL)

**Target**: Q1 2027

**Theme**: Promote limensafe from "deterministic detector against
org-defined vocabulary" to "deterministic + statistical detection
across the surfaces that matter for AI-builder organizations." Public
release follows once this lands with stable adoption.

Directional features (the maintainer team + prodmktg will pace):

### Detection

- **NER plug-in** — Presidio sidecar or ONNX-runtime named-entity
  recognition for person/organization/location detection in
  unstructured content where catalog matching is insufficient. CGO=0
  via ONNX runtime or sidecar process (preserves the design pillar).
- **Statistical co-occurrence** — beyond the v0 window-based co-occur
  rules, statistical correlation between candidate tokens so the
  scanner can flag triangulation without an explicit rule for every
  combination.

### Extractors

- **AI artifacts** — chat transcripts, prompt files, agent memory
  files, completion logs. The scenario that started this project.
- **Notebook outputs** — `.ipynb` cell outputs (not just source);
  these often carry training-data leakage that source-only scanning
  misses.
- **Office docs** — `.docx`, `.xlsx`, `.pptx`, `.pdf` content
  extraction. Limited to text layers, not image OCR.
- **Container image layers** — scan the file system of a built
  container image (not just the source). Catches injected fixtures
  and build-time leak markers.

### Distribution + public posture

- **First public release** — repo flips public; signed binaries
  available on releases + Homebrew tap + scoop bucket.
- **Public-facing positioning** — prodmktg pass for brand voice,
  README narrative, example campaigns showing the consulting-firm
  and AI-builder use cases.
- **OSS license clarity** — currently Apache-2.0; confirm before
  public.

### Backlog candidates (not v0.1.0-committed)

- `limensafe sanitize` — interactive rewriter that proposes safe
  replacements for findings
- `limensafe export` — generate goneat / pre-commit / GitHub Actions
  configuration from a `.limensafe/config.yaml`
- VS Code / Cursor IDE extension surfacing findings live as you type

## v1.0 — Control plane (DIRECTIONAL only; not committed)

**Target**: TBD; depends on enterprise pull

The L4 control plane sketched in early design docs: catalog
distribution + signed bundles + tenant-scoped policy + audit log of
catalog updates. Specifically punted from v0/v1 because the local CLI

- env-var path covers the design partner use case today. Will revisit
  if multi-org / multi-tenant pull signals emerge.

## How this roadmap changes

- the maintainer team owns the roadmap from v0.0.4 onward; PRs against this file
  are the change mechanism
- v0.0.3 / v0.0.4 timeline edits welcome
- Larger directional changes (v0.1.0, v1.0) should be discussed in
  `the internal coordination channel` and ideally aligned with prodmktg before
  landing
- This file does NOT contain raw protected vocabulary, codenames, or
  client identities (it's a public-tier doc per the project's own
  rules)
