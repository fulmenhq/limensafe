# Maintainers — limensafe

**Project**: limensafe — Confidential Context Leakage (CCL) detector
**Repository**: [fulmenhq/limensafe](https://github.com/fulmenhq/limensafe) (private; intended public after first signed releases)
**Governance**: 3 Leaps Initiative

## Human Maintainers

### @3leapsdave (Dave Thompson)

- **Role**: Project Lead, Committer-of-Record, and currently the sole human maintainer
- **Responsibilities**: Architecture oversight, release sign-off, supervision of all AI agent contributions, GitHub account-of-record for the fulmenhq org
- **Contact**: <dave@3leaps.net> · GitHub [@3leapsdave](https://github.com/3leapsdave) · X [@3leapsdave](https://x.com/3leapsdave)

Through the v0.x cycle, every commit and release operation against this
repository flows through this account. Agent GitHub accounts are
expected to come online over the next several months, at which point
agent commits will appear under their own handles and this file will
gain additional human + agent entries.

## Agent Maintainers (supervised)

All AI agents listed below operate under explicit supervision by
@3leapsdave. Agent commits use the 3 Leaps attribution standard:
`noreply@3leaps.net` Co-Authored-By, `Role:` trailer matching the
operating role, and `Committer-of-Record:` set to @3leapsdave for
supervised commits. See [`CONTRIBUTING.md`](CONTRIBUTING.md#commit-attribution-3-leaps-standard).

### the maintainer team — Developer Tools cluster (post-v0.0.3 owner)

From v0.0.4 onward, limensafe sits within the maintainer team's repo cluster
(other the maintainer team repos: `goneat`, `brooklyn-mcp`, `a sibling repo`, `a sibling repo`,
`a sibling repo`, `fulmen-toolbox`). Reasoning: limensafe is a developer-
experience tool whose primary integration partner (`goneat`) is
already a the maintainer team repo.

| Agent handle         | Role    | Responsibilities                                                                    |
| -------------------- | ------- | ----------------------------------------------------------------------------------- |
| `devlead` | devlead | Implementation, architecture, feature work. Channel owner of `the internal coordination channel`. |
| `devrev`  | devrev  | Code review, bug finding, four-eyes audit on PRs                                    |
| `uxdev`   | uxdev   | CLI UX, error-message quality, docs polish for users                                |

### Org-spanning agents (cross-cutting, v0.x cycle)

These agents are not the maintainer team-scoped but contributed substantially to
the v0.x cycle and remain on-call for the surfaces they shipped.

| Agent handle             | Role     | Surface they own                                                                       |
| ------------------------ | -------- | -------------------------------------------------------------------------------------- |
| `cxotech` | cxotech  | v0 spike + v0.0.2 + v0.0.3 design and execution; handoff coordinator                   |
| `entarch` | entarch  | Release signing (PGP + minisign); shared signing patterns with goneat/a sibling repo          |
| `dispatch`         | dispatch | Channel routing, agent provisioning, role assignment                                   |
| `devlead`    | devlead  | Original beta-tester (partner-integration, DataWidget integration); UX/contract feedback consumer |
| `devrev`     | devrev   | Original beta-tester (partner-integration)                                                          |
| `secrev`  | secrev   | Security review (zero-leak boundary cases, sanitization)                               |
| `releng`  | releng   | Release engineering; works with entarch on signing posture                             |

## Operating Modes

### Supervised mode (current, through v0.x)

- All agent work requires human review before commit
- @3leapsdave is Committer-of-Record on every commit
- Per-commit attribution trailers include the agent handle, role, and supervisor (see [`CONTRIBUTING.md`](CONTRIBUTING.md))
- Branch protection on `main`: required_approving_review_count=0 (single-account self-approval works); enforce_admins=false; force-push and deletion disabled

### Autonomous mode (future, v1.x or later)

When agent GitHub accounts come online and the team is comfortable
with autonomous boundaries:

- Agents commit under their own GitHub identities
- Defined boundaries enforced via branch protection + CODEOWNERS
- Escalation contact for boundary cases: @3leapsdave
- Commits get `Autonomous-Agent:` and `Escalation-Contact:` trailers

## Attribution Guidelines

Follow the [Git Commit Attribution Baseline](docs/catalog/agentic/attribution/git-commit.md) and the corresponding section in [`CONTRIBUTING.md`](CONTRIBUTING.md#commit-attribution-3-leaps-standard).

### Required Trailers

```
Co-Authored-By: <Model display name> <noreply@3leaps.net>
Role: <role>
Committer-of-Record: Dave Thompson <dave.thompson@3leaps.net> [@3leapsdave]
```

### Key Requirements

- Use `noreply@3leaps.net` (NEVER vendor defaults like `noreply@anthropic.com`)
- Include `Role:` trailer matching the operating role from the catalog
- Include `Committer-of-Record:` for human accountability while operating in supervised mode

## Roles

This repository uses the FulmenHQ Crucible role catalog. Role prompts
are referenced from [`config/agentic/roles/`](config/agentic/roles/).

| Role       | Use When                                                         |
| ---------- | ---------------------------------------------------------------- |
| `devlead`  | Implementation, architecture, feature work                       |
| `devrev`   | Code review, bug finding, four-eyes audit                        |
| `infoarch` | Documentation, schemas, standards                                |
| `prodmktg` | Release notes, README updates, public messaging (pre-public)     |
| `cicd`     | Pipelines, builds, release automation                            |
| `secrev`   | Security review (zero-leak surfaces, sanitization, supply chain) |
| `releng`   | Release engineering, signing, versioning                         |
| `uxdev`    | CLI UX, error-message quality, docs polish for users             |
| `cxotech`  | Cross-cutting platform decisions (org-spanning, v0.x only)       |
| `entarch`  | Enterprise architecture (org-spanning, v0.x only)                |

## Channels (Mattermost — org-fulmenhq)

| Channel                              | Purpose                                                                                           |
| ------------------------------------ | ------------------------------------------------------------------------------------------------- |
| `the internal coordination channel`                | Persistent ops channel for cross-cutting decisions, status broadcasts, handoff coordination       |
| `the brief channel`                           | Brief-specific implementation channels (one active per repo by default; internal-SOP)               |
| `#solution-planning-context-leakage` | Design council (cxotech + entarch + dispatch + dave) — v0 design history; quiet post-v0.0.3       |
| `the brief channel`                      | DataWidget integration beta-test channel (india + cxotech + entarch + secrev + dispatch + dave) |
| `the team channel`                         | the maintainer team-wide channel — broader team context across all the maintainer team repos                               |
| `the dispatch channel`                   | Org-wide dispatch / coordination                                                                  |
| `the architecture review channel`                    | Org-wide architecture review                                                                      |

## Governance Structure

- Human maintainers approve architecture, releases, and supervise AI agents
- AI agents execute tasks under defined roles with human oversight
- See [`REPOSITORY_SAFETY_PROTOCOLS.md`](REPOSITORY_SAFETY_PROTOCOLS.md) for guardrails and escalation paths

## Escalation

- **Build / CI issues**: `the internal coordination channel` with `devlead`
- **Security concerns (e.g., suspected vocab leak in output)**: `the internal coordination channel` with `secrev` + `@3leapsdave`
- **Release-blocking bugs**: `the internal coordination channel` with `@3leapsdave`
- **Architecture / boundary changes**: `the architecture review channel` or `the internal coordination channel`
- **Catalog / governance questions** (post-v1, when control plane lands): TBD; tentatively `the internal coordination channel`

## Provenance

This file was refit from the groningen workhorse template during the
v0.0.3 handoff slate (slice #6) — adapted the supervised-agent
governance pattern and added the the maintainer team + org-spanning agent
listings. Future updates: humans add their own entries via PR; agent
GitHub accounts get listed under their own handle once provisioned.
