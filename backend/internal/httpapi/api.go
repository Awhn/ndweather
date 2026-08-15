package httpapi

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ndweather/backend/internal/config"
	"ndweather/backend/internal/ingest"
	"ndweather/backend/internal/store"
)

type API struct {
	C      config.Config
	S      *store.Store
	I      *ingest.Service
	Static http.Handler
}
type envelope struct {
	APIVersion  string `json:"apiVersion"`
	GeneratedAt string `json:"generatedAt"`
	Data        any    `json:"data"`
	Status      string `json:"status,omitempty"`
}

func write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(envelope{"v1", time.Now().UTC().Format(time.RFC3339), data, ""})
}
func (a *API) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "live"}) })
	m.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if e := a.S.Ready(r.Context()); e != nil {
			write(w, 503, map[string]string{"status": "not_ready"})
			return
		}
		if a.I != nil {
			if e := a.I.Ready(); e != nil {
				write(w, 503, map[string]string{"status": "not_ready"})
				return
			}
		}
		write(w, 200, map[string]string{"status": "ready"})
	})
	if a.C.IngestMode == "http" {
		m.HandleFunc("POST /api/v1/ingest/batches", a.ingest)
	}
	m.HandleFunc("GET /api/v1/sites", a.read(a.sites))
	m.HandleFunc("GET /api/v1/observations/latest", a.read(a.latestObservations))
	m.HandleFunc("GET /api/v1/observations", a.read(a.observations))
	m.HandleFunc("GET /api/v1/forecasts/latest", a.read(a.latestForecasts))
	m.HandleFunc("GET /api/v1/warnings/active", a.read(a.warnings))
	m.HandleFunc("GET /api/v1/radar/latest", a.read(a.radarLatest))
	m.HandleFunc("GET /api/v1/radar/frames", a.read(a.radarFrames))
	m.HandleFunc("GET /api/v1/typhoons/active", a.read(a.typhoons))
	m.HandleFunc("GET /api/v1/status", a.read(a.status))
	m.HandleFunc("GET /api/v1/display/dashboard", a.read(a.dashboard))
	m.HandleFunc("GET /assets/weather/{name}", a.read(a.asset))
	m.HandleFunc("GET /display", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "web/index.html") })
	m.Handle("GET /", a.Static)
	return a.middleware(m)
}
func secureEqual(got, want string) bool {
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
func bearer(r *http.Request) string {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
func (a *API) read(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.C.ReadToken != "" && !secureEqual(bearer(r), a.C.ReadToken) {
			write(w, 401, map[string]string{"code": "UNAUTHORIZED"})
			return
		}
		next(w, r)
	}
}
func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self'; script-src 'self'; connect-src 'self'")
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "request_id", id, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}
func (a *API) ingest(w http.ResponseWriter, r *http.Request) {
	if !secureEqual(bearer(r), a.C.IngestToken) {
		write(w, 401, map[string]string{"code": "UNAUTHORIZED"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(a.C.MaxRequestMB)<<20)
	b, e := io.ReadAll(r.Body)
	if e != nil {
		write(w, 413, map[string]string{"code": "REQUEST_TOO_LARGE"})
		return
	}
	batch, e := ingest.DecodeJSON(b)
	if e != nil {
		a.failure(w, batch.BatchID, e)
		return
	}
	res, e := a.I.Process(r.Context(), batch)
	if e != nil {
		a.failure(w, batch.BatchID, e)
		return
	}
	write(w, 200, res)
}
func (a *API) failure(w http.ResponseWriter, bid string, e error) {
	code := "INTERNAL_ERROR"
	status := 400
	if x, ok := e.(*ingest.Error); ok {
		code = x.Code
	} else {
		status = 500
	}
	_, _ = a.S.DB.Exec(`INSERT INTO ingest_errors(batch_id,error_code,message,received_at) VALUES(?,?,?,?)`, bid, code, "batch rejected", time.Now().UTC().Format(time.RFC3339))
	write(w, status, map[string]string{"code": code, "message": "batch rejected"})
}
func (a *API) sites(w http.ResponseWriter, r *http.Request) {
	x, e := a.S.Sites()
	if e != nil {
		write(w, 500, []any{})
		return
	}
	write(w, 200, x)
}
func serverError(w http.ResponseWriter) { write(w, 500, map[string]string{"code": "INTERNAL_ERROR"}) }
func limit(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n < 1 {
		n = 100
	}
	if n > 500 {
		n = 500
	}
	return n
}
func rowsJSON(db *sql.DB, q string, args ...any) ([]map[string]any, error) {
	rows, e := db.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			return nil, e
		}
		m := map[string]any{}
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				m[c] = string(b)
			} else {
				m[c] = vals[i]
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (a *API) latestObservations(w http.ResponseWriter, r *http.Request) {
	x, e := rowsJSON(a.S.DB, `SELECT o.* FROM observations o JOIN (SELECT site_code,max(observed_at) t FROM observations GROUP BY site_code) x ON x.site_code=o.site_code AND x.t=o.observed_at ORDER BY o.site_code`)
	if e != nil {
		write(w, 500, []any{})
		return
	}
	write(w, 200, x)
}
func (a *API) observations(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	if since == "" {
		since = time.Now().AddDate(0, 0, -7).UTC().Format(time.RFC3339)
	}
	if _, e := time.Parse(time.RFC3339, since); e != nil {
		write(w, 400, map[string]string{"code": "INVALID_TIME"})
		return
	}
	x, e := rowsJSON(a.S.DB, `SELECT * FROM observations WHERE observed_at>=? ORDER BY observed_at DESC LIMIT ?`, since, limit(r))
	if e != nil {
		serverError(w)
		return
	}
	write(w, 200, x)
}

const latestForecastSQL = `SELECT f.* FROM forecasts f JOIN (
	SELECT site_code,valid_at,max(issued_at) issued_at FROM forecasts WHERE valid_at>=? GROUP BY site_code,valid_at
) x ON x.site_code=f.site_code AND x.valid_at=f.valid_at AND x.issued_at=f.issued_at ORDER BY f.site_code,f.valid_at LIMIT ?`

func (a *API) latestForecasts(w http.ResponseWriter, r *http.Request) {
	x, e := rowsJSON(a.S.DB, latestForecastSQL, time.Now().UTC().Format(time.RFC3339), 500)
	if e != nil {
		serverError(w)
		return
	}
	write(w, 200, x)
}
func (a *API) warnings(w http.ResponseWriter, r *http.Request) {
	x, e := a.activeWarnings(100)
	if e != nil {
		serverError(w)
		return
	}
	write(w, 200, x)
}
func (a *API) radarFrames(w http.ResponseWriter, r *http.Request) {
	x, e := rowsJSON(a.S.DB, `SELECT asset_id,content_type,observed_at,received_at,size,'/assets/weather/'||substr(path,length(rtrim(?, '/'))+2) url FROM radar_assets ORDER BY observed_at DESC LIMIT ?`, a.C.AssetDir, limit(r))
	if e != nil {
		serverError(w)
		return
	}
	write(w, 200, x)
}
func (a *API) radarLatest(w http.ResponseWriter, r *http.Request) {
	r.URL.RawQuery = "limit=1"
	a.radarFrames(w, r)
}
func (a *API) typhoons(w http.ResponseWriter, r *http.Request) {
	x, e := a.activeTyphoons()
	if e != nil {
		serverError(w)
		return
	}
	write(w, 200, x)
}
func (a *API) activeTyphoons() ([]map[string]any, error) {
	x, e := rowsJSON(a.S.DB, `SELECT * FROM typhoons WHERE active=1 ORDER BY announced_at DESC`)
	if e != nil {
		return nil, e
	}
	for _, t := range x {
		pts, pointErr := rowsJSON(a.S.DB, `SELECT forecast_at,latitude,longitude,pressure,max_wind FROM typhoon_forecast_points WHERE typhoon_id=? ORDER BY forecast_at`, t["id"])
		if pointErr != nil {
			return nil, pointErr
		}
		t["forecastPoints"] = pts
	}
	return x, nil
}
func (a *API) activeWarnings(max int) ([]map[string]any, error) {
	x, e := rowsJSON(a.S.DB, `SELECT * FROM warnings WHERE expires_at IS NULL OR expires_at>? ORDER BY CASE level WHEN '경보' THEN 0 WHEN '주의보' THEN 1 ELSE 2 END,announced_at DESC LIMIT ?`, time.Now().UTC().Format(time.RFC3339), max)
	if e != nil {
		return nil, e
	}
	for _, warning := range x {
		sites, siteErr := rowsJSON(a.S.DB, `SELECT site_code FROM warning_sites WHERE warning_id=? ORDER BY site_code`, warning["id"])
		if siteErr != nil {
			return nil, siteErr
		}
		codes := make([]string, 0, len(sites))
		for _, site := range sites {
			if code, ok := site["site_code"].(string); ok {
				codes = append(codes, code)
			}
		}
		warning["site_codes"] = codes
	}
	return x, nil
}
func latestTime(values ...string) string {
	latest := ""
	for _, value := range values {
		if value > latest {
			latest = value
		}
	}
	return latest
}
func stateRank(state string) int {
	if state == "disconnected" {
		return 2
	}
	if state == "stale" {
		return 1
	}
	return 0
}
func (a *API) statusData() (map[string]any, error) {
	var last sql.NullString
	if e := a.S.DB.QueryRow(`SELECT max(received_at) FROM ingest_batches WHERE status='processed'`).Scan(&last); e != nil {
		return nil, e
	}
	expected := []string{}
	if a.C.KMAServiceKey != "" {
		expected = append(expected, "kma_forecast")
	}
	if a.C.KMAAPIHubKey != "" {
		expected = append(expected, "apihub_radar", "apihub_warnings", "apihub_typhoons")
	}
	rows, e := rowsJSON(a.S.DB, `SELECT name,last_attempt_at,last_success_at,last_error IS NOT NULL failed FROM collector_status`)
	if e != nil {
		return nil, e
	}
	byName := map[string]map[string]any{}
	for _, row := range rows {
		if name, ok := row["name"].(string); ok {
			byName[name] = row
		}
	}
	overall := "normal"
	lastSuccess := last.String
	collectors := map[string]any{}
	for _, name := range expected {
		row := byName[name]
		lastSuccessfulAt, _ := row["last_success_at"].(string)
		var parsed time.Time
		if lastSuccessfulAt != "" {
			parsed, _ = time.Parse(time.RFC3339, lastSuccessfulAt)
		}
		state := ingest.IsStale(parsed, time.Now().UTC(), a.C.StaleMinutes)
		if stateRank(state) > stateRank(overall) {
			overall = state
		}
		collectors[name] = map[string]any{"state": state, "lastAttemptAt": row["last_attempt_at"], "lastSuccessfulAt": lastSuccessfulAt, "lastAttemptFailed": row["failed"]}
		lastSuccess = latestTime(lastSuccess, lastSuccessfulAt)
	}
	if len(expected) == 0 {
		var parsed time.Time
		if last.Valid {
			parsed, _ = time.Parse(time.RFC3339, last.String)
		}
		overall = ingest.IsStale(parsed, time.Now().UTC(), a.C.StaleMinutes)
	}
	return map[string]any{"state": overall, "lastSuccessfulReceiveAt": lastSuccess, "demo": a.C.Demo, "rotateSeconds": a.C.RotateSeconds, "refreshSeconds": a.C.RefreshSeconds, "radarFrameSeconds": a.C.RadarSeconds, "integrations": map[string]bool{"forecastEnabled": a.C.KMAServiceKey != "", "apiHubEnabled": a.C.KMAAPIHubKey != ""}, "collectors": collectors}, nil
}
func (a *API) status(w http.ResponseWriter, r *http.Request) {
	status, e := a.statusData()
	if e != nil {
		serverError(w)
		return
	}
	write(w, 200, status)
}
func (a *API) dashboard(w http.ResponseWriter, r *http.Request) {
	sites, e := a.S.Sites()
	if e != nil {
		serverError(w)
		return
	}
	obs, e := rowsJSON(a.S.DB, `SELECT o.* FROM observations o JOIN (SELECT site_code,max(observed_at)t FROM observations GROUP BY site_code)x ON x.site_code=o.site_code AND x.t=o.observed_at ORDER BY o.site_code`)
	if e != nil {
		serverError(w)
		return
	}
	fc, e := rowsJSON(a.S.DB, latestForecastSQL, time.Now().UTC().Format(time.RFC3339), 1000)
	if e != nil {
		serverError(w)
		return
	}
	warn, e := a.activeWarnings(100)
	if e != nil {
		serverError(w)
		return
	}
	rad, e := rowsJSON(a.S.DB, `SELECT * FROM (SELECT asset_id,observed_at,received_at,'/assets/weather/'||substr(path,length(rtrim(?, '/'))+2) url FROM radar_assets ORDER BY observed_at DESC LIMIT 10) ORDER BY observed_at`, a.C.AssetDir)
	if e != nil {
		serverError(w)
		return
	}
	ty, e := a.activeTyphoons()
	if e != nil {
		serverError(w)
		return
	}
	status, e := a.statusData()
	if e != nil {
		serverError(w)
		return
	}
	write(w, 200, map[string]any{"sites": sites, "observations": obs, "forecasts": fc, "warnings": warn, "radarFrames": rad, "typhoons": ty, "status": status})
}
func (a *API) asset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || (!strings.HasSuffix(name, ".png") && !strings.HasSuffix(name, ".jpg")) {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(a.C.AssetDir, name))
}
func StaticHandler() http.Handler {
	if _, e := os.Stat("web"); e != nil {
		return http.NotFoundHandler()
	}
	return http.FileServer(http.Dir(filepath.Clean("web")))
}
