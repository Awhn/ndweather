package apihub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ndweather/backend/internal/store"
)

func TestWarningsUseAPIHubAuthAndNormalize(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/typ01/url/wrn_met_data.php" || r.URL.Query().Get("authKey") != "hub-key" {
			t.Fatalf("request %s", r.URL.String())
		}
		_, _ = w.Write([]byte("# REG_ID TM_ST TM_ED REG_SP REG_UP REG_KO REG_NAME TM_FC TM_EF TM_IN STN WRN LVL CMD GRD CNT RPT STN_ID TM_SEQ MAN_FC MAN_IN\nSAMPLE 0 0 x x sample 샘플지역 202608141000 202608141100 0 0 W 1 1 0 0 0 0 0 x x\n"))
	}))
	defer s.Close()
	loc, _ := time.LoadLocation("Asia/Seoul")
	c := Client{Endpoint: s.URL, AuthKey: "hub-key", HTTP: s.Client(), Location: loc}
	b, err := c.FetchWarnings(context.Background(), []store.Site{{Code: "HQ", Areas: []string{"SAMPLE"}}}, time.Date(2026, 8, 14, 12, 0, 0, 0, loc))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records.Warnings) != 1 || b.Records.Warnings[0].Phenomenon != "강풍" || b.Records.Warnings[0].SiteCodes[0] != "HQ" {
		t.Fatalf("warnings=%+v", b.Records.Warnings)
	}
}

func TestRadarRejectsNonImage(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("authentication error")) }))
	defer s.Close()
	c := Client{Endpoint: s.URL, AuthKey: "bad", HTTP: s.Client()}
	_, err := c.FetchRadar(context.Background(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err=%v", err)
	}
}

func TestTyphoonForecastNormalization(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/typ01/url/typ_now.php" || r.URL.Query().Get("authKey") != "hub-key" {
			t.Fatalf("request %s", r.URL.String())
		}
		_, _ = w.Write([]byte("# FT YY TYP SEQ TMD TYP_TM FT_TM LAT LON DIR SP PS WS TYP_NAME\n0 2026 7 12 0 202608140000 202608140000 25.0 130.0 N 18 980 32 LAN\n1 2026 7 12 12 202608140000 202608141200 27.0 129.0 N 20 985 30 LAN\n"))
	}))
	defer s.Close()
	c := Client{Endpoint: s.URL, AuthKey: "hub-key", HTTP: s.Client()}
	b, err := c.FetchTyphoons(context.Background(), time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records.Typhoons) != 1 || b.Records.Typhoons[0].Pressure == nil || *b.Records.Typhoons[0].Pressure != 980 || len(b.Records.Typhoons[0].ForecastPoints) != 1 {
		t.Fatalf("typhoons=%+v", b.Records.Typhoons)
	}
}
