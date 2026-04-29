# Forge-Workhorse-Limensafe – Maintainers

**Project**: forge-workhorse-limensafe
**Purpose**: Production-ready workhorse application template for robust, scalable Go backends
**Governance Model**: 3leaps Initiative

## Human Maintainers

### @3leapsdave (Dave Thompson)

- **Role**: Project Lead & Primary Maintainer
- **Responsibilities**: Template architecture, workhorse standard compliance, integration oversight, production readiness
- **Contact**: dave.thompson@3leaps.net | GitHub [@3leapsdave](https://github.com/3leapsdave) | X [@3leapsdave](https://x.com/3leapsdave)
- **Supervision**: All AI agent contributions

## Agentic Roles

This repository uses role-based agentic development. Agents operate under roles defined in the [Role Catalog](config/agentic/roles/README.md).

### Available Roles

| Role       | Catalog                                             | Use When                                   |
| ---------- | --------------------------------------------------- | ------------------------------------------ |
| `devlead`  | [devlead.yaml](config/agentic/roles/devlead.yaml)   | Implementation, architecture, feature work |
| `devrev`   | [devrev.yaml](config/agentic/roles/devrev.yaml)     | Code review, bug finding, four-eyes audit  |
| `infoarch` | [infoarch.yaml](config/agentic/roles/infoarch.yaml) | Documentation, schemas, standards          |
| `cicd`     | [cicd.yaml](config/agentic/roles/cicd.yaml)         | Pipelines, builds, automation              |

### Operating Modes

**Supervised Mode** (current):

- All agent work requires human review before commit
- Human maintainer (@3leapsdave) is Committer-of-Record
- See [Git Commit Attribution Baseline](docs/catalog/agentic/attribution/git-commit.md)

**Autonomous Mode** (future):

- Agents operate within defined boundaries
- Escalation contact for issues: @3leapsdave
- Requires `Autonomous-Agent:` and `Escalation-Contact:` trailers

## Attribution Guidelines

Follow the [Git Commit Attribution Baseline](docs/catalog/agentic/attribution/git-commit.md).

### Required Trailers

```
Co-Authored-By: <Model> <noreply@3leaps.net>
Role: <role>
Committer-of-Record: Dave Thompson <dave.thompson@3leaps.net> [@3leapsdave]
```

### Key Requirements

- Use `noreply@3leaps.net` (NOT vendor defaults like `noreply@anthropic.com`)
- Include `Role:` trailer matching the operating role from the catalog
- Include `Committer-of-Record:` for human accountability

## Governance Structure

- Human maintainers approve architecture, releases, and supervise AI agents
- AI agents execute tasks under defined roles with human oversight
- See `REPOSITORY_SAFETY_PROTOCOLS.md` for guardrails and escalation paths

## Communication Channels

- **Primary**: GitHub Issues and Pull Requests
- **Escalation**: Direct contact with @3leapsdave for critical issues

## Contribution Guidelines

All contributors (human and AI) must:

- Follow Fulmen Forge Workhorse Standard
- Follow Go coding standards from Crucible `docs/standards/coding/go.md`
- Maintain test coverage above 80%
- Run `make check-all` before commits
- Document all template features for CDRL users
- Ensure backward compatibility for template consumers
- Coordinate breaking changes with @3leapsdave
