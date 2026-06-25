---
name: audit
description: Add activity logging and model versioning to a togo app with the audit plugin — record changes with field-level diffs, snapshot & restore records, and query the activity log.
---

# togo audit

Use this skill to add an audit trail to a togo app via `togo-framework/audit`.

## Record activity
```go
a, _ := audit.FromKernel(k)
a.Log(ctx, "created", "<subject_type>", id, causerID, map[string]any{...})
a.LogChange(ctx, "updated", "<subject_type>", id, causerID, oldMap, newMap) // auto-diffs
```
Mount points: call `LogChange` in your resource update/delete handlers. Causer
(X-User-Id), IP and User-Agent are captured from the request context by the
bundled `audit.Middleware`.

## Versioning & restore
```go
a.Snapshot(ctx, type, id, currentMap, causerID) // before a destructive change
data, _, _ := a.Restore(ctx, versionID)         // get prior state to re-apply
```

## Query
`a.Activity(ctx, audit.Filter{SubjectType, SubjectID, CauserID, Action, Limit, Offset})`
or the REST endpoints under `/api/audit/*`.

## Notes
- Diffs store only changed fields as `{old,new}` — keep maps to the auditable columns.
- Snapshot before deletes/major updates so Restore has a target.
- The log is append-only; query by subject for a record's history, by causer for a user's actions.
