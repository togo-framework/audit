# audit — docs

The `togo-framework/audit` plugin records an activity log of changes to your
models (who did what, with a field-level diff) plus model versioning + restore.

- **Install:** `togo install togo-framework/audit`
- **Marketplace:** https://to-go.dev/plugins/audit
- **Source:** https://github.com/togo-framework/audit

## Contents
- [Usage](usage.md) — logging activity, diffs, versioning/restore, querying, REST

## Overview
Resolve the service with `audit.FromKernel(k)`. Record activity with `Log` /
`LogChange` (auto-diffs old→new), snapshot subjects with `Snapshot` and restore
with `Restore`. Causer/IP/User-Agent are captured automatically from the request
context via the bundled `Middleware`. See [usage.md](usage.md).
