# DataWidget Usage Examples

> Synthetic corpus fixture — see `../../README.md`. "Acme Corp" is a
> placeholder; this prose deliberately includes leak-shaped content for
> the v0 limensafe spike.

## Connecting to Acme Corp's analytics warehouse

Configure a DataWidget profile against Acme's staging environment:

```bash
datawidget profile create acme-dev \
  --aws-profile acme-dev \
  --description "Acme development"
```

Once the profile is active, list available datasets:

```bash
datawidget datasets list --profile acme-dev
```

## Switching environments

The Acme engagement uses two profiles:

- `acme-dev` for development against the staging warehouse
- `acme-prod` for production read-only access

Switch profiles with `datawidget profile use acme-prod`.

## Internal note (delete before publishing)

The `acme-horizon-dev` profile points at the internal `horizon` cluster.
Reach out to the platform team if you need access.
