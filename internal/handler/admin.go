package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminHandler serves the /health admin interface: system status + log browser.
type AdminHandler struct {
	pool      *pgxpool.Pool
	log       *slog.Logger
	adminUser string
	adminPass string
	startTime time.Time
	tmpls     map[string]*template.Template
}

// NewAdminHandler constructs an AdminHandler with templates parsed from webFS.
func NewAdminHandler(pool *pgxpool.Pool, log *slog.Logger, adminUser, adminPass string, webFS fs.FS) (*AdminHandler, error) {
	tmpls, err := parseAdminTemplates(webFS)
	if err != nil {
		return nil, err
	}
	return &AdminHandler{
		pool:      pool,
		log:       log,
		adminUser: adminUser,
		adminPass: adminPass,
		startTime: time.Now(),
		tmpls:     tmpls,
	}, nil
}

func parseAdminTemplates(webFS fs.FS) (map[string]*template.Template, error) {
	pages := []string{"health"}
	result := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		t, err := template.ParseFS(webFS,
			"templates/base.html",
			"templates/"+page+".html",
		)
		if err != nil {
			return nil, err
		}
		result[page] = t
	}
	// Standalone partial (no base layout).
	partial, err := template.ParseFS(webFS, "templates/health_logs_partial.html")
	if err != nil {
		return nil, err
	}
	result["health_logs_partial"] = partial
	return result, nil
}

// BasicAuth is a middleware that enforces HTTP Basic Auth on admin endpoints.
// If credentials are not configured (empty), it returns 403 with setup instructions.
func (a *AdminHandler) BasicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.adminUser == "" || a.adminPass == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Admin interface is disabled.\nSet ADMIN_USER and ADMIN_PASSWORD environment variables to enable it."))
			return
		}
		user, pass, ok := r.BasicAuth()
		userHash := sha256.Sum256([]byte(user))
		expectedUserHash := sha256.Sum256([]byte(a.adminUser))
		passHash := sha256.Sum256([]byte(pass))
		expectedPassHash := sha256.Sum256([]byte(a.adminPass))

		userMatch := subtle.ConstantTimeCompare(userHash[:], expectedUserHash[:]) == 1
		passMatch := subtle.ConstantTimeCompare(passHash[:], expectedPassHash[:]) == 1

		if !ok || !userMatch || !passMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="disapyr-admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// ─── Health page data ─────────────────────────────────────────────────────────

type healthData struct {
	AppStatus   string
	DBStatus    string
	DBError     string
	DBTotalConn int32
	DBIdleConn  int32
	DBMaxConn   int32
	Uptime      string
	GoVersion   string
	LogSummary  []logLevelCount
	InitialLogs []logRow
	TotalLogs   int

	// Secret activity counters (all-time totals from secret_events).
	SecretsCreated   int64
	SecretsRetrieved int64
	SecretsExpired   int64
}

type logLevelCount struct {
	Level      string
	LevelClass string // lowercase for CSS
	Count      int
}

type logRow struct {
	ID         int64
	TS         time.Time
	Level      string
	LevelClass string // lowercase level for CSS class (e.g. "warn")
	Msg        string
	Attrs      template.JS // safe for rendering raw JSON in a <pre>
}

// ServeHealth renders the full /health admin page.
func (a *AdminHandler) ServeHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	data := healthData{
		AppStatus: "healthy",
		GoVersion: runtime.Version(),
		Uptime:    formatUptime(time.Since(a.startTime)),
	}

	// DB health probe.
	if err := a.pool.Ping(ctx); err != nil {
		data.DBStatus = "unhealthy"
		data.DBError = err.Error()
		a.log.Warn("health check: DB ping failed", "error", err)
	} else {
		data.DBStatus = "healthy"
		stat := a.pool.Stat()
		data.DBTotalConn = stat.TotalConns()
		data.DBIdleConn = stat.IdleConns()
		data.DBMaxConn = stat.MaxConns()
	}

	// Log summary (last 24 h).
	data.LogSummary = a.queryLogSummary(ctx)

	// Secret activity stats (all-time totals).
	data.SecretsCreated, data.SecretsRetrieved, data.SecretsExpired = a.querySecretStats(ctx)

	// Initial log rows (page 1, no filter).
	data.InitialLogs, data.TotalLogs = a.queryLogs(ctx, "", "24h", 0)

	tmpl, ok := a.tmpls["health"]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		a.log.Error("health template execute", "error", err)
	}
}

// ServeHealthLogs is the HTMX endpoint for /health/logs (filtered partial).
func (a *AdminHandler) ServeHealthLogs(w http.ResponseWriter, r *http.Request) {
	level := r.URL.Query().Get("level")
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "24h"
	}
	pageStr := r.URL.Query().Get("page")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, total := a.queryLogs(ctx, level, window, page)

	type partialData struct {
		Rows     []logRow
		Total    int
		Page     int
		NextPage int
		PrevPage int
		Level    string
		Window   string
		HasMore  bool
		PageSize int
		ClearMsg string // empty unless this is a clear response
	}
	const pageSize = 50
	data := partialData{
		Rows:     rows,
		Total:    total,
		Page:     page,
		NextPage: page + 1,
		PrevPage: page - 1,
		Level:    level,
		Window:   window,
		HasMore:  (page+1)*pageSize < total,
		PageSize: pageSize,
	}

	tmpl, ok := a.tmpls["health_logs_partial"]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "health_logs_partial.html", data); err != nil {
		a.log.Error("health logs partial execute", "error", err)
	}
}

// ClearLogs handles POST /health/logs/clear.
// Query param older_than: empty = all, "1h"/"6h"/"24h"/"7d" = time-bounded delete.
// Returns an updated log partial (HTMX response).
func (a *AdminHandler) ClearLogs(w http.ResponseWriter, r *http.Request) {
	// CSRF guard: verify Origin/Referer matches the request host.
	if !sameOrigin(r) {
		http.Error(w, "Forbidden: cross-origin request", http.StatusForbidden)
		return
	}

	olderThan := r.FormValue("older_than")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var (
		n   int64
		err error
	)

	if olderThan == "" {
		ct, execErr := a.pool.Exec(ctx, `DELETE FROM app_logs`)
		err = execErr
		if execErr == nil {
			n = ct.RowsAffected()
		}
	} else {
		interval := windowToInterval(olderThan)
		ct, execErr := a.pool.Exec(ctx, `DELETE FROM app_logs WHERE ts < NOW() - $1::interval`, interval)
		err = execErr
		if execErr == nil {
			n = ct.RowsAffected()
		}
	}

	if err != nil {
		a.log.Warn("clear logs failed", "older_than", olderThan, "error", err)
		http.Error(w, "failed to clear logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	a.log.Warn("logs cleared by admin",
		"older_than", olderThan,
		"rows_deleted", n,
		"remote_addr", r.RemoteAddr,
	)

	// Return a refreshed log partial so the browser updates in-place.
	rows, total := a.queryLogs(ctx, "", "24h", 0)

	type partialData struct {
		Rows      []logRow
		Total     int
		Page      int
		NextPage  int
		PrevPage  int
		Level     string
		Window    string
		HasMore   bool
		PageSize  int
		ClearMsg  string
	}
	const pageSize = 50
	msg := ""
	if olderThan == "" {
		msg = "All logs cleared."
	} else {
		msg = "Logs older than " + humanWindow(olderThan) + " cleared."
	}
	data := partialData{
		Rows:     rows,
		Total:    total,
		Page:     0,
		NextPage: 1,
		PrevPage: -1,
		Window:   "24h",
		HasMore:  pageSize < total,
		PageSize: pageSize,
		ClearMsg: msg,
	}

	tmpl, ok := a.tmpls["health_logs_partial"]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "health_logs_partial.html", data); err != nil {
		a.log.Error("clear logs partial execute", "error", err)
	}
}

// humanWindow converts a window string to a human-readable label.
func humanWindow(window string) string {
	switch window {
	case "1h":
		return "1 hour"
	case "6h":
		return "6 hours"
	case "7d":
		return "7 days"
	default:
		return "24 hours"
	}
}

// ─── DB queries ───────────────────────────────────────────────────────────────

// querySecretStats returns all-time totals for created, retrieved, and expired
// secret events. Errors are silently swallowed — the health page renders with
// zero counts rather than failing.
func (a *AdminHandler) querySecretStats(ctx context.Context) (created, retrieved, expired int64) {
	rows, err := a.pool.Query(ctx,
		`SELECT event_type, COALESCE(SUM(count), 0)
		 FROM secret_events
		 GROUP BY event_type`)
	if err != nil {
		a.log.Warn("querySecretStats", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var eventType string
		var total int64
		if err := rows.Scan(&eventType, &total); err != nil {
			continue
		}
		switch eventType {
		case "created":
			created = total
		case "retrieved":
			retrieved = total
		case "expired":
			expired = total
		}
	}
	if err := rows.Err(); err != nil {
		a.log.Warn("querySecretStats iteration", "error", err)
	}
	return
}

func (a *AdminHandler) queryLogSummary(ctx context.Context) []logLevelCount {
	const q = `
		SELECT level, COUNT(*) AS cnt
		FROM app_logs
		WHERE ts > NOW() - INTERVAL '24 hours'
		GROUP BY level
		ORDER BY cnt DESC`

	rows, err := a.pool.Query(ctx, q)
	if err != nil {
		a.log.Warn("queryLogSummary", "error", err)
		return nil
	}
	defer rows.Close()

	var out []logLevelCount
	for rows.Next() {
		var lc logLevelCount
		if err := rows.Scan(&lc.Level, &lc.Count); err == nil {
			lc.LevelClass = strings.ToLower(lc.Level)
			out = append(out, lc)
		}
	}
	return out
}

func (a *AdminHandler) queryLogs(ctx context.Context, level, window string, page int) ([]logRow, int) {
	const pageSize = 50
	offset := page * pageSize

	// Map window string to SQL interval.
	interval := windowToInterval(window)

	const countQ = `
		SELECT COUNT(*)
		FROM app_logs
		WHERE ($1::text = '' OR level = $1)
		  AND ts > NOW() - $2::interval`

	var total int
	if err := a.pool.QueryRow(ctx, countQ, level, interval).Scan(&total); err != nil {
		a.log.Warn("queryLogs count", "error", err)
		return nil, 0
	}

	const rowQ = `
		SELECT id, ts, level, msg, attrs
		FROM app_logs
		WHERE ($1::text = '' OR level = $1)
		  AND ts > NOW() - $2::interval
		ORDER BY ts DESC
		LIMIT $3 OFFSET $4`

	pgRows, err := a.pool.Query(ctx, rowQ, level, interval, pageSize, offset)
	if err != nil {
		a.log.Warn("queryLogs rows", "error", err)
		return nil, total
	}
	defer pgRows.Close()

	var out []logRow
	for pgRows.Next() {
		var row logRow
		var attrsRaw []byte
		if err := pgRows.Scan(&row.ID, &row.TS, &row.Level, &row.Msg, &attrsRaw); err != nil {
			continue
		}
		row.LevelClass = strings.ToLower(row.Level)
		// Pretty-print attrs JSON for the template.
		if len(attrsRaw) > 0 {
			var pretty interface{}
			if json.Unmarshal(attrsRaw, &pretty) == nil {
				b, _ := json.MarshalIndent(pretty, "", "  ")
				row.Attrs = template.JS(b) //nolint:gosec // safe: rendered inside <pre>, not eval'd
			} else {
				row.Attrs = template.JS(attrsRaw)
			}
		}
		out = append(out, row)
	}
	return out, total
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func windowToInterval(window string) string {
	switch window {
	case "1h":
		return "1 hour"
	case "6h":
		return "6 hours"
	case "7d":
		return "7 days"
	default:
		return "24 hours"
	}
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	if days > 0 {
		return strconv.Itoa(days) + "d " + strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	}
	if hours > 0 {
		return strconv.Itoa(hours) + "h " + strconv.Itoa(minutes) + "m"
	}
	return strconv.Itoa(minutes) + "m " + strconv.Itoa(seconds) + "s"
}

// sameOrigin returns true when the request's Origin or Referer header matches
// the request Host. This is a lightweight CSRF guard for admin POST endpoints
// protected by Basic Auth (where credentials can be cached by the browser).
// Requests with no Origin AND no Referer are allowed (e.g. direct API calls,
// curl — the admin already authenticated via Basic Auth).
func sameOrigin(r *http.Request) bool {
	check := func(raw string) bool {
		u, err := url.Parse(raw)
		if err != nil {
			return false
		}
		// Compare host (strip default ports for robustness).
		return strings.EqualFold(u.Host, r.Host) ||
			strings.EqualFold(u.Hostname(), r.Host)
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		return check(origin)
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		return check(ref)
	}
	// No Origin/Referer — allow (direct/curl calls).
	return true
}
