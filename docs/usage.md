# audit — usage

## Resolve the service
```go
a, _ := audit.FromKernel(k)
```

## Log activity
```go
// Explicit changeset:
a.Log(ctx, "created", "post", post.ID, userID, map[string]any{"title": post.Title})

// Auto-diff old → new (stores only changed fields as {old,new}):
a.LogChange(ctx, "updated", "post", post.ID, userID, oldMap, newMap)
```
Causer / IP / User-Agent are filled from the request context when you mount the
`audit.Middleware` (the plugin mounts it on the kernel router automatically).

## Versioning & restore
```go
a.Snapshot(ctx, "post", post.ID, oldMap, userID) // store a point-in-time snapshot
vs, _ := a.Versions(ctx, "post", post.ID)         // list snapshots (newest first)
data, ver, _ := a.Restore(ctx, vs[1].ID)          // get a snapshot's data to re-apply
```
`Restore` returns the stored data — apply it to your live record (audit owns
history, not your tables).

## Query the log
```go
a.Activity(ctx, audit.Filter{SubjectType: "post", SubjectID: "p1"})
a.Activity(ctx, audit.Filter{CauserID: "u1", Action: "deleted", Limit: 50, Offset: 0})
```

## REST endpoints
| Method | Path | Purpose |
|---|---|---|
| GET | `/api/audit/activity?subject_type=&subject_id=&causer_id=&action=&limit=&offset=` | filtered activity |
| GET | `/api/audit/subjects/{type}/{id}` | activity for a subject |
| GET | `/api/audit/versions/{type}/{id}` | snapshots for a subject |
| POST | `/api/audit/versions/{id}/restore` | snapshot data to re-apply |
| GET | `/api/audit/dashboard` | recent activity feed |

## Schema
`activity_log` (action, subject_type, subject_id, causer_id, changes JSON, ip,
user_agent, created_at; indexed by subject/causer/action) and `versions`
(subject + data JSON snapshot). Created automatically on boot.
