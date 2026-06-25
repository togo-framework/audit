// Package audit records an activity log of changes to your models/resources —
// who did what to which subject, with a field-level diff — plus optional model
// versioning with restore. It is the togo answer to Spatie Activitylog /
// PaperTrail / django-reversion.
//
//	a, _ := audit.FromKernel(k)
//	a.LogChange(ctx, "updated", "post", post.ID, userID, oldPost, newPost) // diffs old→new
//	a.Snapshot(ctx, "post", post.ID, oldPost, userID)                      // version for restore
package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/togo-framework/orm"
	"github.com/togo-framework/togo"
)

// JSONText is a JSON value stored in a text column. It scans from string/[]byte
// and marshals back to raw JSON (so API responses nest objects, not strings).
type JSONText []byte

// Scan implements sql.Scanner.
func (j *JSONText) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = []byte("null")
	case string:
		*j = []byte(v)
	case []byte:
		*j = append((*j)[:0], v...)
	default:
		return fmt.Errorf("audit: cannot scan %T into JSONText", src)
	}
	if len(*j) == 0 {
		*j = []byte("null")
	}
	return nil
}

// MarshalJSON emits the raw JSON.
func (j JSONText) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// Map decodes the JSON into a map (nil on error).
func (j JSONText) Map() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(j, &m)
	return m
}

// Entry is one activity-log record.
type Entry struct {
	ID          string   `db:"id" json:"id"`
	Action      string   `db:"action" json:"action"`
	SubjectType string   `db:"subject_type" json:"subject_type"`
	SubjectID   string   `db:"subject_id" json:"subject_id"`
	CauserID    string   `db:"causer_id" json:"causer_id"`
	Changes     JSONText `db:"changes" json:"changes"`
	IP          string   `db:"ip" json:"ip,omitempty"`
	UserAgent   string   `db:"user_agent" json:"user_agent,omitempty"`
	CreatedAt   string   `db:"created_at" json:"created_at"`
}

// Version is a stored snapshot of a subject, used to restore prior state.
type Version struct {
	ID          string   `db:"id" json:"id"`
	SubjectType string   `db:"subject_type" json:"subject_type"`
	SubjectID   string   `db:"subject_id" json:"subject_id"`
	Data        JSONText `db:"data" json:"data"`
	CauserID    string   `db:"causer_id" json:"causer_id,omitempty"`
	CreatedAt   string   `db:"created_at" json:"created_at"`
}

// Service is the audit runtime stored on the kernel (k.Get("audit")).
type Service struct {
	k       *togo.Kernel
	sqldb   *sql.DB     // test override; nil → use k.SQL
	dialect orm.Dialect // test override; nil → use k.Dialect
}

func init() {
	togo.RegisterProviderFunc("audit", togo.PriorityLate+10, func(k *togo.Kernel) error {
		s := &Service{k: k, dialect: k.Dialect()}
		if err := s.migrate(context.Background()); err != nil {
			return err
		}
		k.Set("audit", s)
		s.routes()
		return nil
	})
}

// FromKernel returns the audit Service registered on the kernel.
func FromKernel(k *togo.Kernel) (*Service, bool) {
	v, ok := k.Get("audit")
	if !ok {
		return nil, false
	}
	s, ok := v.(*Service)
	return s, ok
}

func (s *Service) db(ctx context.Context) (*sql.DB, error) {
	if s.sqldb != nil {
		return s.sqldb, nil
	}
	return s.k.SQL(ctx)
}

func (s *Service) log(db *sql.DB) *orm.Query[Entry] {
	return orm.For[Entry](db, s.dialect, "activity_log")
}
func (s *Service) ver(db *sql.DB) *orm.Query[Version] {
	return orm.For[Version](db, s.dialect, "versions")
}

func (s *Service) migrate(ctx context.Context) error {
	db, err := s.db(ctx)
	if err != nil {
		return err
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS activity_log (id text PRIMARY KEY, action text NOT NULL, subject_type text NOT NULL DEFAULT '', subject_id text NOT NULL DEFAULT '', causer_id text NOT NULL DEFAULT '', changes text NOT NULL DEFAULT '{}', ip text NOT NULL DEFAULT '', user_agent text NOT NULL DEFAULT '', created_at text NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_subject ON activity_log (subject_type, subject_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_causer ON activity_log (causer_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_activity_action ON activity_log (action, created_at)`,
		`CREATE TABLE IF NOT EXISTS versions (id text PRIMARY KEY, subject_type text NOT NULL, subject_id text NOT NULL, data text NOT NULL DEFAULT '{}', causer_id text NOT NULL DEFAULT '', created_at text NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_versions_subject ON versions (subject_type, subject_id, created_at)`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	return nil
}

// Log writes an activity-log entry. changes is an arbitrary changeset (often the
// output of Diff). IP / user-agent / causer are filled from the request context
// (see Middleware) when not supplied.
func (s *Service) Log(ctx context.Context, action, subjectType, subjectID, causerID string, changes map[string]any) (*Entry, error) {
	db, err := s.db(ctx)
	if err != nil {
		return nil, err
	}
	if m := metaFromContext(ctx); m != nil {
		if causerID == "" {
			causerID = m.CauserID
		}
	}
	raw, _ := json.Marshal(changes)
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	data := map[string]any{
		"id": newID(), "action": action, "subject_type": subjectType,
		"subject_id": subjectID, "causer_id": causerID, "changes": string(raw),
		"created_at": nowRFC(),
	}
	if m := metaFromContext(ctx); m != nil {
		data["ip"] = m.IP
		data["user_agent"] = m.UserAgent
	}
	return s.log(db).Create(ctx, data)
}

// LogChange diffs old→new and records the changeset as an "updated"-style entry.
func (s *Service) LogChange(ctx context.Context, action, subjectType, subjectID, causerID string, old, new map[string]any) (*Entry, error) {
	return s.Log(ctx, action, subjectType, subjectID, causerID, Diff(old, new))
}

// Change is a single field's before/after.
type Change struct {
	Old any `json:"old"`
	New any `json:"new"`
}

// Diff returns a changeset of fields that differ between old and new
// (added/removed/changed), each as {old,new}.
func Diff(old, new map[string]any) map[string]any {
	out := map[string]any{}
	for k, nv := range new {
		ov, had := old[k]
		if !had || !jsonEqual(ov, nv) {
			out[k] = Change{Old: ov, New: nv}
		}
	}
	for k, ov := range old {
		if _, had := new[k]; !had {
			out[k] = Change{Old: ov, New: nil}
		}
	}
	return out
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// Filter narrows an activity query.
type Filter struct {
	SubjectType string
	SubjectID   string
	CauserID    string
	Action      string
	Limit       int
	Offset      int
}

// Activity returns activity-log entries matching the filter, newest first.
func (s *Service) Activity(ctx context.Context, f Filter) ([]Entry, error) {
	db, err := s.db(ctx)
	if err != nil {
		return nil, err
	}
	q := s.log(db)
	if f.SubjectType != "" {
		q = q.Where("subject_type", "=", f.SubjectType)
	}
	if f.SubjectID != "" {
		q = q.Where("subject_id", "=", f.SubjectID)
	}
	if f.CauserID != "" {
		q = q.Where("causer_id", "=", f.CauserID)
	}
	if f.Action != "" {
		q = q.Where("action", "=", f.Action)
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return q.Order("created_at DESC").Limit(limit).Offset(f.Offset).Get(ctx)
}

// Snapshot stores a version (point-in-time snapshot) of a subject for restore.
func (s *Service) Snapshot(ctx context.Context, subjectType, subjectID string, data map[string]any, causerID string) (*Version, error) {
	db, err := s.db(ctx)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data)
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	return s.ver(db).Create(ctx, map[string]any{
		"id": newID(), "subject_type": subjectType, "subject_id": subjectID,
		"data": string(raw), "causer_id": causerID, "created_at": nowRFC(),
	})
}

// Versions lists snapshots for a subject, newest first.
func (s *Service) Versions(ctx context.Context, subjectType, subjectID string) ([]Version, error) {
	db, err := s.db(ctx)
	if err != nil {
		return nil, err
	}
	return s.ver(db).
		Where("subject_type", "=", subjectType).
		Where("subject_id", "=", subjectID).
		Order("created_at DESC").Get(ctx)
}

// Restore returns the snapshot data for a version id. The caller applies it to
// the live record (audit owns history, not your tables).
func (s *Service) Restore(ctx context.Context, versionID string) (map[string]any, *Version, error) {
	db, err := s.db(ctx)
	if err != nil {
		return nil, nil, err
	}
	v, err := s.ver(db).Find(ctx, versionID)
	if err != nil {
		return nil, nil, err
	}
	var out map[string]any
	_ = json.Unmarshal(v.Data, &out)
	return out, v, nil
}

// ----- request-context meta (causer / ip / user-agent) -----

type metaKey struct{}

// Meta carries the actor + request info captured for an entry.
type Meta struct {
	CauserID  string
	IP        string
	UserAgent string
}

// WithMeta attaches actor/request metadata to a context so Log can pick it up.
func WithMeta(ctx context.Context, m Meta) context.Context {
	return context.WithValue(ctx, metaKey{}, m)
}

func metaFromContext(ctx context.Context) *Meta {
	if m, ok := ctx.Value(metaKey{}).(Meta); ok {
		return &m
	}
	return nil
}

// Middleware captures the causer (X-User-Id header), client IP and user-agent
// into the request context so subsequent audit.Log calls auto-fill them.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := Meta{
			CauserID:  r.Header.Get("X-User-Id"),
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		}
		next.ServeHTTP(w, r.WithContext(WithMeta(r.Context(), m)))
	})
}

// ----- helpers -----

func newID() string  { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func nowRFC() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ----- REST -----

func (s *Service) routes() {
	r := s.k.Router
	if r == nil {
		return
	}
	r.Use(Middleware)
	const base = "/api/audit"
	r.Get(base+"/activity", s.handleActivity)
	r.Get(base+"/subjects/{type}/{id}", s.handleSubject)
	r.Get(base+"/versions/{type}/{id}", s.handleVersions)
	r.Post(base+"/versions/{id}/restore", s.handleRestore)
	r.Get(base+"/dashboard", s.handleDashboard)
}

func (s *Service) handleActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	entries, err := s.Activity(r.Context(), Filter{
		SubjectType: q.Get("subject_type"), SubjectID: q.Get("subject_id"),
		CauserID: q.Get("causer_id"), Action: q.Get("action"),
		Limit: limit, Offset: offset,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Service) handleSubject(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Activity(r.Context(), Filter{
		SubjectType: chi.URLParam(r, "type"), SubjectID: chi.URLParam(r, "id"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Service) handleVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := s.Versions(r.Context(), chi.URLParam(r, "type"), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": vs})
}

func (s *Service) handleRestore(w http.ResponseWriter, r *http.Request) {
	data, v, err := s.Restore(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": v, "data": data})
}

func (s *Service) handleDashboard(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Activity(r.Context(), Filter{Limit: 50})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recent": entries, "count": len(entries)})
}
