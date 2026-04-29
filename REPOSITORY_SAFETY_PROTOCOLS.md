# Forge-Workhorse-Limensafe – Repository Safety Protocols

This document outlines the safety protocols for forge-workhorse-limensafe repository operations.

## Quick Reference

- **Human Oversight Required**: All merges, tags, and publishes need @3leapsdave approval
- **Use Make Targets**: Prefer `make` commands for consistency and safety
- **Plan Changes**: Document work plans in `.plans/` before structural changes
- **Template Philosophy**: Remember this is a template - changes affect all CDRL users
- **Incident Response**: Follow escalation process to @3leapsdave for critical issues

## Template-Specific Safety Guidelines

### CDRL Workflow Protection

- **Identity File**: `.fulmen/app.yaml` is sacred - changes must preserve CDRL workflow
- **Documentation**: README CDRL section must stay current with template changes
- **Examples**: Verify `.env.example` completeness before release
- **Testing**: Always test CDRL workflow (clone → degit → refit → launch)

### Integration Safety

- **gofulmen Coupling**: When locally linked, remember to switch back to GitHub release before tagging
- **Breaking Changes**: Template changes that break downstream CDRL users require major version bump
- **Backward Compatibility**: Old environment variables and config paths should continue working

### Test Coverage

- **Minimum Coverage**: Maintain existing coverage levels (currently internal/observability)
- **New Features**: Must include tests before merge
- **Integration Tests**: Add tests for gofulmen module integration
- **Manual Testing**: CLI commands must be manually tested

### Dependency Management

- **gofulmen Version**: Coordinate updates with gofulmen releases
- **Crucible Version**: Transitive via gofulmen, document version in RELEASE_NOTES
- **Security Scanning**: Monitor dependencies for vulnerabilities
- **Minimal Dependencies**: Prefer gofulmen modules over external dependencies

## High-Risk Operations

### Version Bumps

- **Process**: Update VERSION constant in relevant files
- **Verification**: Run `make test && make build` after version bump
- **Approval**: Major version bumps require @3leapsdave approval
- **Communication**: Coordinate with template users if breaking changes

### Release Operations

- **Pre-Release**: Complete RELEASE_CHECKLIST.md before tagging
- **Tagging**: Only @3leapsdave can create release tags
- **Publishing**: Verify GitHub release and `go get` work post-publish
- **CDRL Test**: Always test complete CDRL workflow before release

### Structural Changes

- **Package Reorganization**: Document in feature briefs, get approval
- **CLI Changes**: Breaking CLI changes require deprecation period
- **Config Changes**: New config keys must have defaults, old keys still work

### gofulmen Integration

- **Local Development**: OK to use local replace during active development
- **Pre-Release**: Must switch back to GitHub release before tagging
- **Version Coordination**: Ensure gofulmen version is released before Limensafe release
- **Feature Flags**: Consider using feature flags for experimental gofulmen features

## Incident Response

### Build Failures

1. Check `make test` and `make lint` output
2. Fix failing tests or linting issues
3. Verify fix with fresh build
4. Document root cause in commit message
5. Add regression test if applicable

### Test Failures

1. Isolate failing test: `go test -v ./internal/...`
2. Reproduce locally
3. Fix code or update test (as appropriate)
4. Ensure all tests pass before commit
5. Update test documentation if test expectations changed

### Integration Failures

1. Check gofulmen version compatibility
2. Verify `.fulmen/app.yaml` validity
3. Check for API changes in gofulmen
4. Review integration points (logger, metrics, config)
5. Escalate to @3leapsdave if gofulmen issue suspected

### CDRL Workflow Failures

1. Clone template fresh: `git clone <repo> test-instance`
2. Follow CDRL steps exactly as documented in README
3. Identify which step fails
4. Fix template or documentation as appropriate
5. Re-test complete workflow
6. Update README CDRL section if process changed

## Emergency Procedures

### Critical Security Issue

1. **DO NOT commit fixes to public main branch immediately**
2. Contact @3leapsdave via direct channel
3. Create private security branch if needed
4. Coordinate hotfix release process
5. Notify template users after fix is released

### Production-Impacting Bug

1. Assess severity and user impact
2. Create hotfix branch: `hotfix/v<version>`
3. Implement minimal fix with tests
4. Fast-track review with @3leapsdave
5. Release hotfix following RELEASE_CHECKLIST
6. Document root cause in `.plans/post-mortems/`

### Dependency Vulnerability

1. Check severity via `go list -m all | nancy sleuth` (or similar)
2. Review if vulnerability affects template or only downstream users
3. Update dependency if fix available
4. Test thoroughly - dependencies affect CDRL users
5. Release patch version if high severity
6. Document in RELEASE_NOTES security section

## Safety Checklist for Common Operations

### Before Every Commit

- [ ] Tests pass: `make test`
- [ ] Code formatted: `make fmt`
- [ ] Lint clean: `make lint`
- [ ] Manual testing done (if CLI changes)
- [ ] Attribution trailers included

### Before Every PR

- [ ] Feature brief in `.plans/active/<version>/` (if major feature)
- [ ] RELEASE_NOTES.md updated
- [ ] README updated (if user-facing changes)
- [ ] Tests cover new functionality
- [ ] No hardcoded application names (use app identity)

### Before Every Release

- [ ] RELEASE_CHECKLIST.md completed
- [ ] gofulmen local replace removed (back to GitHub release)
- [ ] CDRL workflow tested end-to-end
- [ ] No secrets in code or config
- [ ] Dependencies reviewed

## Guardrails

### Automated Protections

- `.plans/` is gitignored (planning files never committed)
- `.env` is gitignored (secrets never committed)
- Make targets enforce quality gates

### Manual Protections

- All releases require @3leapsdave approval
- Breaking changes require deprecation period
- Security issues handled privately first

### Process Protections

- Feature briefs required for major changes
- CDRL workflow tested before every release
- Attribution standard enforced on all commits

## Escalation Paths

### Development Questions

1. Check README and docs/ first
2. Review feature briefs in `.plans/active/`
3. Check gofulmen documentation
4. Ask @3leapsdave

### Technical Blockers

1. Document issue in `.plans/blockers/`
2. Attempt workaround if safe
3. Escalate to @3leapsdave with context
4. Pause work if blocker is critical

### Process Uncertainty

1. When in doubt, ask @3leapsdave
2. Better to pause than proceed incorrectly
3. Document decision rationale
4. Update this document if process gap identified

## References

- `AGENTS.md` - Agent guidelines and startup protocol
- `MAINTAINERS.md` - Agent identities and responsibilities
- `RELEASE_CHECKLIST.md` - Release process checklist
- `RELEASE_NOTES.md` - Release history and notes
- [Fulmen Forge Workhorse Standard](https://github.com/fulmenhq/crucible/blob/main/docs/architecture/fulmen-forge-workhorse-standard.md)
- [Agentic Attribution Standard](https://github.com/fulmenhq/crucible/blob/main/docs/standards/agentic-attribution.md)
