# CI/CD Configuration

This document explains the CI/CD setup for this repository.

## Container-Based CI Pattern

This repository pins the **goneat-tools-runner container** (`ghcr.io/fulmenhq/goneat-tools-runner:v0.5.7`) for Linux CI jobs. Native Linux amd64 and arm64 jobs verify goneat v0.6.1 and the tools listed below; the image tag alone is not a tool-version receipt.

### Why Containers?

The Linux matrix checks these tools inside the image before building:

- `goneat` v0.6.1 - formatting and diagnostics
- `sfetch` - verified tool fetching
- `prettier` - Markdown/JSON formatting
- `yamlfmt` - YAML formatting
- `jq` / `yq` - JSON/YAML processing
- `rg` (ripgrep) - Fast search

The container supplies those tools without installing each one in the workflow. Go and the CI-pinned golangci-lint are set up separately.

### Container Permissions (`--user 1001`)

This template uses `options: --user 1001` for `goneat-tools-runner` container jobs.

```yaml
container:
  image: ghcr.io/fulmenhq/goneat-tools-runner:v0.5.7
  options: --user 1001
```

#### Why 1001?

GitHub Actions mounts the workspace and temp directories into the container under `/__w`. Using UID 1001 aligns with GitHub-hosted runner workspace ownership and avoids `EACCES` errors when actions write state files (e.g. checkout).

If your org uses self-hosted runners with different ownership, adjust the UID accordingly.

### Additional Hardening Patterns

#### Minimize `GITHUB_TOKEN` capabilities

Explicitly declare the workflow `permissions` block (for this template: `contents: read`) so the implicit token cannot mutate repository state even if a step is compromised. This keeps the example aligned with GitHub's least-privilege guidance.

#### Avoid persisting checkout credentials

Pass `persist-credentials: false` to `actions/checkout@v4`. CI jobs in this template never push, so there's no reason to store the short-lived token inside `.git/config`. Downstream users can override when they need to push tags or release artifacts.

#### Enforce strict shell options in scripts

Add `set -euo pipefail` at the top of every multi-line `run` script. This catches unset variables, stops on the first failing command, and prevents silent formatting or build failures inside the container.

### CI Jobs

1. **format-check**: Validates formatting using container tools (yamlfmt, prettier)
2. **build-test**: Builds and tests the application using container tools + goneat
3. **bootstrap-smoke**: Exercises the built CLI end to end in the Linux image
4. **native-linux-smoke**: Checks the v0.5.7 image and CLI on native Linux amd64 and arm64
5. **native-host-smoke**: Builds and exercises the CLI natively on Windows amd64/arm64 and Darwin arm64 (no Linux container)
6. **native-platform-gate**: Fails unless both matrix jobs succeed across all five cells

The `*-s` runner labels name GitHub-provisioned ephemeral hosted runners registered for the estate. `.github/actionlint.yaml` only lets actionlint parse those custom labels; its `self-hosted-runner` key does not mean these machines are self-hosted. CI asserts the actual host architecture and Go patch (at least 1.25.13) rather than trusting the label or `1.25.x` selector alone. The five-platform result is required before merging or releasing.

Note: `actions/setup-go` selects Go 1.25.x inside the container jobs. CI explicitly installs its tested golangci-lint v2.4.0 pin with `golangci-lint-action` instead of relying on the image's newer version.

### Local Development

For local development, you have two options:

1. **Use the container** (recommended for consistency):

   ```bash
   docker run --rm -v "$(pwd)":/work -w /work --entrypoint "" \
     ghcr.io/fulmenhq/goneat-tools-runner:v0.5.7 yamlfmt -lint .
   ```

2. **Install tools locally via sfetch + goneat**:

   ```bash
   # Install the trust anchor (sfetch)
   curl -sSfL https://github.com/3leaps/sfetch/releases/latest/download/install-sfetch.sh | bash -s -- --yes --dir "$HOME/.local/bin"
   export PATH="$HOME/.local/bin:$PATH"

   # Verify sfetch install (trust anchor)
   sfetch --self-verify

   # Install goneat via sfetch
   sfetch --repo fulmenhq/goneat --tag v0.6.1 --dest-dir "$HOME/.local/bin"

   # Install foundation tools via goneat
   goneat doctor tools --scope foundation --install --install-package-managers --yes
   ```

## References

- [fulmen-toolbox (goneat-tools-runner image source)](https://github.com/fulmenhq/fulmen-toolbox)
- [goneat documentation](https://github.com/fulmenhq/goneat)
- [GitHub Actions container jobs](https://docs.github.com/en/actions/using-jobs/running-jobs-in-a-container)
