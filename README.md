<div align="center">
  <picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/togo-mark-dark.svg" /><img src=".github/assets/togo-mark.svg" alt="ToGO" height="64" /></picture>
  <h1>togo-framework/audit</h1>
  <p>
    <a href="https://to-go.dev/marketplace"><img src="https://img.shields.io/badge/marketplace-to--go.dev-1F8A99" alt="marketplace" /></a>
    <a href="https://pkg.go.dev/github.com/togo-framework/audit"><img src="https://pkg.go.dev/badge/github.com/togo-framework/audit.svg" alt="pkg.go.dev" /></a>
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT" />
  </p>
  <p><strong>Activity log + model versioning for <a href="https://to-go.dev">togo</a> — who changed what, with diffs and restore.</strong></p>
</div>

## Install

```bash
togo install togo-framework/audit
```

`audit` is the togo answer to Spatie Activitylog / PaperTrail / django-reversion. It records an **activity log** of changes to your models — the action, the subject, the actor (causer), a **field-level diff**, and request IP/User-Agent — plus optional **model versioning** with restore.

## Features

- **Activity log** — `Log` / `LogChange` write append-only entries to `activity_log`, indexed by subject, causer and action.
- **Automatic diffs** — `LogChange(old, new)` stores only the fields that changed, as `{old, new}`.
- **Actor capture** — the bundled `Middleware` pulls the causer (`X-User-Id`), client IP and User-Agent from the request into context, so logs are attributed automatically.
- **Model versioning** — `Snapshot` stores point-in-time JSON snapshots; `Restore` returns prior state to re-apply.
- **Queryable** — filter by subject / causer / action / time, with pagination, over a Go API and REST.

## Usage

```go
import "github.com/togo-framework/audit"

a, _ := audit.FromKernel(k)

// Log an action with an explicit changeset:
a.Log(ctx, "created", "post", post.ID, userID, map[string]any{"title": post.Title})

// Or auto-diff old → new (stores only changed fields):
a.LogChange(ctx, "updated", "post", post.ID, userID, oldMap, newMap)

// Versioning + restore:
a.Snapshot(ctx, "post", post.ID, oldMap, userID) // snapshot before a change
data, ver, _ := a.Restore(ctx, versionID)         // get prior state to re-apply

// Query the trail:
a.Activity(ctx, audit.Filter{SubjectType: "post", SubjectID: post.ID})
a.Activity(ctx, audit.Filter{CauserID: userID, Action: "deleted", Limit: 50})
```

Causer / IP / User-Agent are filled from the request context automatically (the plugin mounts `audit.Middleware` on the kernel router).

### REST API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/audit/activity` | filtered activity (`subject_type`, `subject_id`, `causer_id`, `action`, `limit`, `offset`) |
| `GET` | `/api/audit/subjects/{type}/{id}` | a record's full history |
| `GET` | `/api/audit/versions/{type}/{id}` | snapshots for a subject |
| `POST` | `/api/audit/versions/{id}/restore` | snapshot data to re-apply |
| `GET` | `/api/audit/dashboard` | recent activity feed |

## Configuration

No required env. Audit creates its tables (`activity_log`, `versions`) on boot via the kernel database (works on SQLite/Postgres/MySQL through togo's ORM). High-volume deployments can partition `activity_log` by `created_at`.

---

<div align="center">
  <h3>Premium sponsors</h3>
  <p>
    <a href="https://id8media.com"><strong>ID8 Media</strong></a> &nbsp;·&nbsp;
    <a href="https://one-studio.co"><strong>One Studio</strong></a>
  </p>
  <p><sub>Support togo — <a href="https://github.com/sponsors/fadymondy">become a sponsor</a>.</sub></p>
</div>
