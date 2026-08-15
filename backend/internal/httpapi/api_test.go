package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ndweather/backend/internal/config"
	"ndweather/backend/internal/ingest"
	"ndweather/backend/internal/store"
)

func testAPI(t *testing.T, c config.Config) *API {
	t.Helper()
	dir := t.TempDir()
	sites := filepath.Join(dir, "sites.yaml")
	contents := "- code: SAMPLE\n  name: Sample\n  latitude: 1\n  longitude: 1\n  forecastGridX: 1\n  forecastGridY: 1\n  warningAreaCodes: []\n  displayOrder: 1\n  enabled: true\n"
	if err := os.WriteFile(sites, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dir, "weather.db"), sites)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DB.Close() })
	assetDir := filepath.Join(dir, "assets")
	if err = os.MkdirAll(assetDir, 0750); err != nil {
		t.Fatal(err)
	}
	c.AssetDir = assetDir
	if c.StaleMinutes == 0 {
		c.StaleMinutes = 20
	}
	return &API{C: c, S: s, I: &ingest.Service{Store: s, AssetDir: assetDir, MaxAssetBytes: 100}, Static: http.NotFoundHandler()}
}

func request(handler http.Handler, method, path, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestDirectoryModeDoesNotExposeHTTPIngest(t *testing.T) {
	a := testAPI(t, config.Config{IngestMode: "directory"})
	w := request(a.Handler(), http.MethodPost, "/api/v1/ingest/batches", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}

func TestReadTokenProtectsDashboardAndAssets(t *testing.T) {
	a := testAPI(t, config.Config{IngestMode: "http", IngestToken: "0123456789abcdef", ReadToken: "read-secret"})
	h := a.Handler()
	if got := request(h, http.MethodGet, "/api/v1/display/dashboard", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("dashboard without token = %d", got)
	}
	if got := request(h, http.MethodGet, "/api/v1/display/dashboard", "read-secret").Code; got != http.StatusUnauthorized {
		t.Fatalf("raw token = %d", got)
	}
	if got := request(h, http.MethodGet, "/api/v1/display/dashboard", "Bearer read-secret").Code; got != http.StatusOK {
		t.Fatalf("dashboard with token = %d", got)
	}
	if err := os.WriteFile(filepath.Join(a.C.AssetDir, "frame.png"), []byte("image"), 0640); err != nil {
		t.Fatal(err)
	}
	if got := request(h, http.MethodGet, "/assets/weather/frame.png", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("asset without token = %d", got)
	}
	if got := request(h, http.MethodGet, "/assets/weather/frame.png", "Bearer read-secret").Code; got != http.StatusOK {
		t.Fatalf("asset with token = %d", got)
	}
}

func TestLatestForecastReturnsOnlyNewestIssue(t *testing.T) {
	a := testAPI(t, config.Config{IngestMode: "http", IngestToken: "0123456789abcdef"})
	now := time.Now().UTC().Truncate(time.Second)
	valid := now.Add(time.Hour).Format(time.RFC3339)
	oldIssue := now.Add(-2 * time.Hour).Format(time.RFC3339)
	newIssue := now.Add(-time.Hour).Format(time.RFC3339)
	for _, issue := range []string{oldIssue, newIssue} {
		if _, err := a.S.DB.Exec(`INSERT INTO forecasts(site_code,issued_at,valid_at,received_at) VALUES(?,?,?,?)`, "SAMPLE", issue, valid, now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	w := request(a.Handler(), http.MethodGet, "/api/v1/forecasts/latest", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0]["issued_at"] != newIssue {
		t.Fatalf("forecasts=%+v", envelope.Data)
	}
}

func TestStatusReportsConfiguredCollector(t *testing.T) {
	a := testAPI(t, config.Config{IngestMode: "http", IngestToken: "0123456789abcdef", KMAServiceKey: "key"})
	if err := a.S.RecordCollector("kma_forecast", nil); err != nil {
		t.Fatal(err)
	}
	w := request(a.Handler(), http.MethodGet, "/api/v1/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data struct {
			State        string                    `json:"state"`
			Integrations map[string]bool           `json:"integrations"`
			Collectors   map[string]map[string]any `json:"collectors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.State != "normal" || !envelope.Data.Integrations["forecastEnabled"] || envelope.Data.Collectors["kma_forecast"]["lastSuccessfulAt"] == "" {
		t.Fatalf("status data=%+v", envelope.Data)
	}
}
