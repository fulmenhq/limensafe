# Role Catalog

Baseline role prompts for AI agent sessions in the FulmenHQ ecosystem.

**Schema**: [`role-prompt.schema.json`](../../../schemas/upstream/3leaps/agentic/v0/role-prompt.schema.json) (vendored from [3leaps/crucible](https://github.com/3leaps/crucible))

## Available Roles

| Role                                   | Slug       | Category   | Purpose                                      |
| -------------------------------------- | ---------- | ---------- | -------------------------------------------- |
| [Development Lead](devlead.yaml)       | `devlead`  | agentic    | Implementation, architecture                 |
| [Development Reviewer](devrev.yaml)    | `devrev`   | review     | Four-eyes code review                        |
| [Information Architect](infoarch.yaml) | `infoarch` | agentic    | Documentation, schemas                       |
| [Enterprise Architect](entarch.yaml)   | `entarch`  | governance | Cross-repo coordination, ecosystem alignment |
| [CI/CD Automation](cicd.yaml)          | `cicd`     | automation | Pipelines, GitHub Actions                    |
| [Product Marketing](prodmktg.yaml)     | `prodmktg` | marketing  | Release notes, README updates, messaging     |
| [Security Review](secrev.yaml)         | `secrev`   | review     | Security analysis, vulnerabilities           |
| [Data Engineering](dataeng.yaml)       | `dataeng`  | agentic    | Database design, data pipelines              |

## FulmenHQ Extensions

These roles extend the [3leaps baseline](https://github.com/3leaps/crucible/tree/main/config/agentic/roles):

| Role      | Extension Purpose                                        |
| --------- | -------------------------------------------------------- |
| `devlead` | Adds FulmenHQ ecosystem patterns                         |
| `devrev`  | Four-eyes code review (FulmenHQ-specific)                |
| `entarch` | Cross-repo coordination (FulmenHQ-specific)              |
| `dataeng` | Enterprise-scale data infrastructure (FulmenHQ-specific) |

## Usage

Reference roles by slug in `AGENTS.md`:

```markdown
## Roles

| Role      | Prompt                                            | Notes           |
| --------- | ------------------------------------------------- | --------------- |
| `devlead` | [devlead.yaml](config/agentic/roles/devlead.yaml) | Implementation  |
| `secrev`  | [secrev.yaml](config/agentic/roles/secrev.yaml)   | Security review |
```

## Schema Validation

All role files conform to the [role-prompt schema](../../../schemas/upstream/3leaps/agentic/v0/role-prompt.schema.json).

Validate with:

```bash
# Using goneat
goneat schema validate --schema schemas/upstream/3leaps/agentic/v0/role-prompt.schema.json config/agentic/roles/*.yaml
```

## Extending Roles

To extend a baseline role:

```yaml
slug: devlead
extends: https://schemas.3leaps.dev/roles/devlead.yaml
# Add or override fields
scope:
  - ...additional scope items...
```

## References

- [Role Catalog README (legacy docs)](../../../docs/catalog/agentic/roles/README.md) - Migration documentation
- [3leaps baseline roles](https://github.com/3leaps/crucible/tree/main/config/agentic/roles) - Upstream baseline
- [Agent Identity Standard](../../../docs/standards/ai-agents.md) - Operating modes and identity scheme
