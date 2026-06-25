package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/togo-framework/orm"
	_ "modernc.org/sqlite"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{sqldb: db, dialect: orm.DialectFor("sqlite")}
	if err := s.migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

func TestLogAndQuery(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if _, err := s.Log(ctx, "created", "post", "p1", "u1", map[string]any{"title": "Hi"}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if _, err := s.Log(ctx, "updated", "post", "p1", "u2", map[string]any{"title": "Yo"}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if _, err := s.Log(ctx, "deleted", "comment", "c9", "u1", nil); err != nil {
		t.Fatalf("Log nil changes: %v", err)
	}

	all, err := s.Activity(ctx, Filter{})
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 entries, got %d", len(all))
	}
	// nil changes must still be valid JSON ("{}").
	for _, e := range all {
		if len(e.Changes) == 0 || !json.Valid(e.Changes) {
			t.Errorf("entry %s has invalid changes JSON: %q", e.ID, e.Changes)
		}
	}

	// Filter by subject.
	bySubject, _ := s.Activity(ctx, Filter{SubjectType: "post", SubjectID: "p1"})
	if len(bySubject) != 2 {
		t.Errorf("subject filter: want 2, got %d", len(bySubject))
	}
	// Filter by causer.
	byCauser, _ := s.Activity(ctx, Filter{CauserID: "u1"})
	if len(byCauser) != 2 {
		t.Errorf("causer filter: want 2, got %d", len(byCauser))
	}
	// Filter by action.
	byAction, _ := s.Activity(ctx, Filter{Action: "updated"})
	if len(byAction) != 1 || byAction[0].SubjectID != "p1" {
		t.Errorf("action filter: %+v", byAction)
	}
}

func TestDiff(t *testing.T) {
	old := map[string]any{"title": "A", "views": 1.0, "tag": "x"}
	new := map[string]any{"title": "B", "views": 1.0, "extra": true}
	d := Diff(old, new)
	// title changed, tag removed, extra added; views unchanged → excluded.
	if _, ok := d["views"]; ok {
		t.Error("unchanged field should not appear in diff")
	}
	for _, f := range []string{"title", "tag", "extra"} {
		if _, ok := d[f]; !ok {
			t.Errorf("expected %q in diff, got %+v", f, d)
		}
	}
	ch, ok := d["title"].(Change)
	if !ok || ch.Old != "A" || ch.New != "B" {
		t.Errorf("title change = %+v", d["title"])
	}
}

func TestLogChangeStoresDiff(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	_, err := s.LogChange(ctx, "updated", "post", "p1", "u1",
		map[string]any{"title": "A"}, map[string]any{"title": "B"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Activity(ctx, Filter{SubjectID: "p1"})
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	var changes map[string]any
	if err := json.Unmarshal(got[0].Changes, &changes); err != nil {
		t.Fatalf("changes not valid JSON: %v", err)
	}
	if _, ok := changes["title"]; !ok {
		t.Errorf("diff not stored: %v", changes)
	}
}

func TestVersioningAndRestore(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	v1, err := s.Snapshot(ctx, "post", "p1", map[string]any{"title": "v1", "body": "first"}, "u1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := s.Snapshot(ctx, "post", "p1", map[string]any{"title": "v2"}, "u1"); err != nil {
		t.Fatal(err)
	}
	vs, err := s.Versions(ctx, "post", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("want 2 versions, got %d", len(vs))
	}
	// Restore the first snapshot.
	data, v, err := s.Restore(ctx, v1.ID)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if v.ID != v1.ID || data["title"] != "v1" || data["body"] != "first" {
		t.Errorf("restored data = %+v (version %s)", data, v.ID)
	}
}

func TestWithMetaFillsCauser(t *testing.T) {
	s := newTestService(t)
	ctx := WithMeta(context.Background(), Meta{CauserID: "ctx-user", IP: "1.2.3.4", UserAgent: "test"})
	e, err := s.Log(ctx, "created", "post", "p1", "", nil) // empty causer → from ctx
	if err != nil {
		t.Fatal(err)
	}
	if e.CauserID != "ctx-user" {
		t.Errorf("causer not filled from context: %q", e.CauserID)
	}
	got, _ := s.Activity(ctx, Filter{SubjectID: "p1"})
	if len(got) != 1 || got[0].IP != "1.2.3.4" || got[0].UserAgent != "test" {
		t.Errorf("meta not persisted: %+v", got)
	}
}
