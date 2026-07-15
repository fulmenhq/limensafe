# Maintainers — limensafe

**Project**: limensafe — Confidential Context Leakage (CCL) detector
**Repository**: [fulmenhq/limensafe](https://github.com/fulmenhq/limensafe)
**Governance**: 3 Leaps Initiative / FulmenHQ

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

## Contact

- **Security reports**: <security@fulmenhq.dev> (see [`SECURITY.md`](SECURITY.md) if present)
- **Bugs, features, and general questions**: open a [GitHub issue](https://github.com/fulmenhq/limensafe/issues) and tag the maintainer (@3leapsdave)

## Agent Maintainers (supervised)

limensafe is developed with AI agents operating under explicit
supervision by @3leapsdave. Agent commits use the 3 Leaps attribution
standard: `noreply@3leaps.net` Co-Authored-By, a `Role:` trailer
matching the operating role, and `Committer-of-Record:` set to
@3leapsdave for supervised commits. See
[`CONTRIBUTING.md`](CONTRIBUTING.md#commit-attribution-3-leaps-standard).

limensafe is maintained by the FulmenHQ team and sits within the
FulmenHQ project family alongside sibling developer-tooling projects
such as `goneat` and `brooklyn-mcp`. limensafe is a developer-experience
tool whose primary integration partner (`goneat`) is a FulmenHQ project.

### Roles / areas of ownership

Ownership is organized by role. Role prompts are referenced from
[`config/agentic/roles/`](config/agentic/roles/).

| Role       | Area of ownership                                                        |
| ---------- | ------------------------------------------------------------------------ |
| `devlead`  | Implementation, architecture, feature work                               |
| `devrev`   | Code review, bug finding, four-eyes audit on PRs                         |
| `uxdev`    | CLI UX, error-message quality, docs polish for users                     |
| `cxotech`  | Cross-cutting platform decisions (org-spanning, v0.x only)               |
| `entarch`  | Release signing (PGP + minisign); shared signing patterns               |
| `secrev`   | Security review (zero-leak boundary cases, sanitization, supply chain)   |
| `releng`   | Release engineering, signing, versioning                                 |

## Operating Modes

### Supervised mode (current, through v0.x)

- All agent work requires human review before commit
- @3leapsdave is Committer-of-Record on every commit
- Per-commit attribution trailers include the role and supervisor (see [`CONTRIBUTING.md`](CONTRIBUTING.md))
- Branch protection on `main`: required_approving_review_count=0 (single-account self-approval works); enforce_admins=false; force-push and deletion disabled

### Autonomous mode (future, v1.x or later)

When agent GitHub accounts come online and the maintainers are
comfortable with autonomous boundaries:

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
| `prodmktg` | Release notes, README updates, public messaging                  |
| `cicd`     | Pipelines, builds, release automation                            |
| `secrev`   | Security review (zero-leak surfaces, sanitization, supply chain) |
| `releng`   | Release engineering, signing, versioning                         |
| `uxdev`    | CLI UX, error-message quality, docs polish for users             |
| `cxotech`  | Cross-cutting platform decisions (org-spanning, v0.x only)       |
| `entarch`  | Enterprise architecture (org-spanning, v0.x only)                |

## Governance Structure

- Human maintainers approve architecture, releases, and supervise AI agents
- AI agents execute tasks under defined roles with human oversight
- See [`REPOSITORY_SAFETY_PROTOCOLS.md`](REPOSITORY_SAFETY_PROTOCOLS.md) for guardrails and escalation paths

## Escalation

- **Build / CI issues**: open a GitHub issue and tag @3leapsdave
- **Security concerns (e.g., suspected vocab leak in output)**: email <security@fulmenhq.dev>; do not file a public issue for undisclosed vulnerabilities
- **Release-blocking bugs**: open a GitHub issue and tag @3leapsdave
- **Architecture / boundary changes**: open a GitHub issue and tag @3leapsdave
