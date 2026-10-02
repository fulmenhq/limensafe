# CDRL Guide: Refitting forge-workhorse-limensafe

This guide explains how to customize the forge-workhorse-limensafe template for your own application using the CDRL (Clone → Degit → Refit → Launch) workflow.

## CDRL Workflow Overview

1. **Clone**: `git clone` the template repository
2. **Degit**: Remove git history to start fresh
3. **Refit**: Customize the template for your application
4. **Launch**: Run your customized application

## Step-by-Step Refitting

### Step 1: Clone the Template

```bash
git clone https://github.com/fulmenhq/limensafe.git my-app
cd my-app
```

### Step 2: Degit (Remove Git History)

```bash
rm -rf .git
git init
```

### Step 3: Refit (Customize for Your App)

#### 3.1 Update App Identity (Most Important)

Edit `.fulmen/app.yaml` to match your application:

```yaml
# Update these values for your application
vendor: mycompany # Your organization
binary_name: myapi # Your application name
service_type: workhorse # Keep this for workhorse templates
env_prefix: MYAPI_ # Your env var prefix (uppercase, include trailing underscore)
config_name: myapi # Your config file name
description: "My API service for processing data"
version: 1.0.0
```

**Why this matters**: The app identity file controls:

- Environment variable prefixes (`MYAPI_*` instead of `LIMENSAFE_*`)
- Configuration file paths (`~/.config/mycompany/myapi.yaml`)
- Telemetry namespaces in logs and metrics
- Binary name and description in CLI help and HTTP responses

**Important**: `description` is shown in CLI help output. Update it to match your app (otherwise you'll see template wording like "workhorse").

#### Release Signing Env Var Prefix (Recommended)

This template’s manual release signing workflow supports app-scoped env vars of the form `<APP>_<VAR>` (plus a few generic fallbacks). After CDRL refit, you should update these to match your new application name.

Example: if your binary is `myapi`, prefer:

```bash
export MYAPI_MINISIGN_KEY=/path/to/myapi.key
export MYAPI_MINISIGN_PUB=/path/to/myapi.pub
export MYAPI_PGP_KEY_ID="security@mycompany.com"   # optional
export MYAPI_GPG_HOMEDIR=/path/to/gnupg-mycompany   # required for tag signing or explicit PGP ceremony
export RELEASE_TAG=v1.0.0
```

The Make targets also honor `SIGNING_ENV_PREFIX` (defaults to `$(BINARY_NAME)` uppercased) if you need to override the inferred `<APP>` prefix.

#### 3.2 Update Module Path

Edit `go.mod`:

```go
module github.com/mycompany/myapi
```

#### Optional: Reset `VERSION`

If you’re starting a new product/repo from this template, reset the template version:

```bash
echo "0.1.0" > VERSION
```

#### Optional: Purge Template-Only Docs

After CDRL, you can delete template-only release history and template overview docs.

**Safe to delete (template history):**

```bash
rm -f CHANGELOG.md
rm -f RELEASE_NOTES.md
rm -rf docs/releases/
```

**Often safe to delete/replace (template-specific docs):**

```bash
rm -f docs/limensafe-overview.md
rm -rf docs/architecture/decisions/
```

**Edit (do not delete) if you keep the Fulmen workflow:**

- `RELEASE_CHECKLIST.md` (update release/signing conventions)
- `REPOSITORY_SAFETY_PROTOCOLS.md` (adapt to your org)
- `AGENTS.md` and `MAINTAINERS.md` (replace maintainers/agent roster)
- `.github/workflows/*` and `scripts/*` (only if you keep the automated release process)

#### Recommended: Verify No Remaining Template References

After refit, confirm you don’t still reference template identifiers (including env prefix residue):

```bash
rg -n "limensafe|LIMENSAFE_" --glob '*.{go,md,yaml,yml,json,mod,sum}' --glob 'Makefile'
```

If the output is non-empty, update the remaining references (common places: CLI help text, tests, Make targets, config comments).

#### Common Residue Hotspots

Based on downstream CDRL runjournals, these are the most common places where template strings persist even after updating `.fulmen/app.yaml`:

| File                      | Location              | What to Change                                                                    |
| ------------------------- | --------------------- | --------------------------------------------------------------------------------- |
| `internal/cmd/envinfo.go` | Line ~22              | `"=== Limensafe Environment Information ==="` → use app identity or your app name |
| `internal/cmd/root.go`    | `Short`/`Long` fields | Root command description in help output                                           |
| `internal/cmd/*.go`       | Various               | Any hardcoded "limensafe" in command descriptions                                 |
| `internal/cmd/*_test.go`  | Test assertions       | References to `LIMENSAFE_*` env vars                                              |

**Tip**: The `envinfo` header is a common miss. Update it to use the app identity dynamically:

```go
// Before (hardcoded)
observability.CLILogger.Info("=== Limensafe Environment Information ===")

// After (dynamic)
observability.CLILogger.Info(fmt.Sprintf("=== %s Environment Information ===", identity.BinaryName))
```

#### 3.3 Update Environment Variables

Copy `.env.example` to `.env` and customize:

```bash
# Your app will now use MYAPI_* prefix
MYAPI_PORT=8080
MYAPI_HOST=localhost
MYAPI_LOG_LEVEL=info
```

#### 3.4 Update Configuration and Schema Files

**Important**: Limensafe uses a versioned config and schema structure that must be renamed during CDRL to match your application identity.

**Rename configuration defaults:**

```bash
# Rename the config category directory
mv config/limensafe config/myapi
# Result: config/myapi/v1.0.0/limensafe-defaults.yaml

# Rename the defaults file to match your app
mv config/myapi/v1.0.0/limensafe-defaults.yaml config/myapi/v1.0.0/myapi-defaults.yaml
```

**Rename schema files:**

```bash
# Rename the schema category directory
mv schemas/limensafe schemas/myapi
# Result: schemas/myapi/v1.0.0/config.schema.json
```

**Update loader configuration** in `internal/config/loader.go`:

```go
// Change Category from "limensafe" to your app name
opts := gfconfig.LayeredConfigOptions{
    Category:     "myapi",  // ← Change this
    Version:      "v1.0.0",
    DefaultsFile: "myapi-defaults.yaml",  // ← Change this
    SchemaID:     "myapi/v1.0.0/config",  // ← Change this
    // ... rest of options
}
```

**Why this matters**: The config loader uses the category name to find your defaults, and the schema ID must match the directory structure for validation to work correctly.

#### 3.5 Customize Application Logic

Replace placeholder business logic in `internal/core/`:

```bash
# Remove template examples
rm -rf internal/core/

# Add your business logic
mkdir -p internal/core
# Add your handlers, services, models here
```

#### 3.6 Update Documentation

Update `README.md`:

- Change "limensafe" to your app name
- Update description and examples
- Add your own usage instructions

Update CLI command descriptions in `internal/cmd/*.go`:

```go
// Before
Short: "Start the HTTP server"

// After
Short: "Start the My API HTTP server"
```

#### 3.7 Update Metadata

Update these files:

- `LICENSE`: Choose your license
- `MAINTAINERS.md`: Add your maintainers
- `README.md`: Your app's documentation

### Step 4: Launch Your Application

```bash
# Install dependencies
make bootstrap

# Run your application
make run

# Or build and run
make build
./bin/myapi serve
```

## Verification Checklist

After refitting, verify these work:

- [ ] App identity loads from `.fulmen/app.yaml`
- [ ] Environment variables use your prefix (`MYAPI_*`)
- [ ] Config defaults directory renamed (`config/myapi/v1.0.0/`)
- [ ] Schema directory renamed (`schemas/myapi/v1.0.0/`)
- [ ] Loader configuration updated in `internal/config/loader.go`
- [ ] Config file loads from correct path
- [ ] CLI shows your app name in help
- [ ] HTTP `/version` endpoint shows your app name
- [ ] Logs use your telemetry namespace
- [ ] Tests pass with `make test`

## Common Refitting Patterns

### Web API Service

```yaml
vendor: acme
binary_name: user-service
env_prefix: USERSVC
config_name: user-service
description: "User management API service"
```

### Data Processing Worker

```yaml
vendor: datatech
binary_name: etl-processor
env_prefix: ETL
config_name: etl-processor
description: "ETL data processing worker"
```

### CLI Tool

```yaml
vendor: toolcorp
binary_name: db-migrator
env_prefix: DBMIG
config_name: db-migrator
description: "Database migration CLI tool"
```

## Troubleshooting

### App Identity Not Loading

```bash
# Check file exists
ls -la .fulmen/app.yaml

# Validate YAML syntax
go run github.com/fulmenhq/goneat@latest validate .fulmen/app.yaml
```

### Environment Variables Not Working

```bash
# Check prefix matches app.yaml
grep env_prefix .fulmen/app.yaml
env | grep USERSVC  # Should show your vars
```

### Config File Not Found

```bash
# Check expected path
ls -la ~/.config/yourcompany/yourapp.yaml

# Or specify custom path
yourapp serve --config ./config/custom.yaml
```

## Advanced Customization

### Adding New Commands

1. Create `internal/cmd/mycommand.go`
2. Add to `rootCmd.AddCommand()` in `init()`
3. Update `Makefile` if needed

### Adding New HTTP Endpoints

1. Create handler in `internal/server/handlers/`
2. Register route in `internal/server/routes.go`
3. Add middleware if needed

### Custom Configuration

1. Add config fields to `setDefaults()` in `internal/cmd/root.go`
2. Update `.env.example` with new variables
3. Document in README

## Best Practices

1. **Keep App Identity Consistent**: All customization flows from `.fulmen/app.yaml`
2. **Update Documentation**: Keep README and help text current
3. **Test Customizations**: Run `make test` after major changes
4. **Version Management**: Use semantic versioning in app identity
5. **Security**: Never commit secrets or API keys
6. **Release Signing**: Remove any Fulmen-provided signatures and sign your own release artifacts before distribution

## Support

- **Template Issues**: Report to [forge-workhorse-limensafe](https://github.com/fulmenhq/limensafe)
- **Fulmen Ecosystem**: See [FulmenHQ Documentation](https://fulmenhq.dev)
- **gofulmen Library**: See [gofulmen](https://github.com/fulmenhq/gofulmen)

---

**Remember**: The goal of CDRL is to make template customization predictable and repeatable. The app identity system ensures most customization happens in one place (`.fulmen/app.yaml`) rather than scattered throughout the codebase.
