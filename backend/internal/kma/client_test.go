package kma

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ndweather/backend/internal/store"
)

func TestFetchVilageForecast(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getVilageFcst" || r.URL.Query().Get("serviceKey") != "test/key" || r.URL.Query().Get("nx") != "81" || r.URL.Query().Get("ny") != "75" || r.URL.Query().Get("dataType") != "JSON" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"header":{"resultCode":"00","resultMsg":"NORMAL_SERVICE"},"body":{"items":{"item":[{"category":"TMP","fcstDate":"20260814","fcstTime":"1200","fcstValue":"27"},{"category":"REH","fcstDate":"20260814","fcstTime":"1200","fcstValue":"61"},{"category":"SKY","fcstDate":"20260814","fcstTime":"1200","fcstValue":"1"},{"category":"POP","fcstDate":"20260814","fcstTime":"1200","fcstValue":"20"},{"category":"TMN","fcstDate":"20260814","fcstTime":"0600","fcstValue":"21"},{"category":"TMX","fcstDate":"20260814","fcstTime":"1500","fcstValue":"30"}]}}}}`))
	}))
	defer server.Close()
	loc, _ := time.LoadLocation("Asia/Seoul")
	c := Client{Endpoint: server.URL, ServiceKey: "test%2Fkey", HTTP: server.Client(), Location: loc}
	b, err := c.Fetch(context.Background(), store.Site{Code: "HQ", GridX: 81, GridY: 75}, time.Date(2026, 8, 14, 11, 20, 0, 0, loc))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Records.Observations) != 1 || len(b.Records.Forecasts) != 2 {
		t.Fatalf("unexpected normalized records: %+v", b.Records)
	}
	if got := *b.Records.Observations[0].Temperature; got != 27 {
		t.Fatalf("temperature = %v", got)
	}
	if b.Records.Forecasts[0].MinTemperature == nil || b.Records.Forecasts[0].MaxTemperature == nil {
		t.Fatal("daily min/max not propagated")
	}
}

func TestLatestBaseWaitsForPublication(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Seoul")
	got := latestBase(time.Date(2026, 8, 14, 8, 5, 0, 0, loc))
	if got.Format("20060102 1504") != "20260814 0500" {
		t.Fatalf("base = %v", got)
	}
}

func TestFetchAcceptsOperationEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gateway/getVilageFcst" {
			t.Fatalf("operation path was appended twice: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"header":{"resultCode":"00","resultMsg":"NORMAL_SERVICE"},"body":{"items":{"item":[{"category":"TMP","fcstDate":"20260814","fcstTime":"1200","fcstValue":"27"}]}}}}`))
	}))
	defer server.Close()
	loc, _ := time.LoadLocation("Asia/Seoul")
	c := Client{Endpoint: server.URL + "/gateway/getVilageFcst", ServiceKey: "key", HTTP: server.Client(), Location: loc}
	if _, err := c.Fetch(context.Background(), store.Site{Code: "HQ", GridX: 81, GridY: 75}, time.Date(2026, 8, 14, 11, 20, 0, 0, loc)); err != nil {
		t.Fatal(err)
	}
}

func TestFetchPreservesPlusInDecodedServiceKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("serviceKey"); got != "test+key" {
			t.Fatalf("serviceKey = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"header":{"resultCode":"00"},"body":{"items":{"item":[{"category":"TMP","fcstDate":"20260814","fcstTime":"1200","fcstValue":"27"}]}}}}`))
	}))
	defer server.Close()
	loc, _ := time.LoadLocation("Asia/Seoul")
	c := Client{Endpoint: server.URL, ServiceKey: "test+key", HTTP: server.Client(), Location: loc}
	if _, err := c.Fetch(context.Background(), store.Site{Code: "HQ", GridX: 81, GridY: 75}, time.Date(2026, 8, 14, 11, 20, 0, 0, loc)); err != nil {
		t.Fatal(err)
	}
}

func TestKMAValueBoundaries(t *testing.T) {
	if got := precipitation("1.0mm 미만"); got != 1.0 {
		t.Fatalf("precipitation = %v, want 1.0", got)
	}
	if got := direction("-999"); got != "" {
		t.Fatalf("missing direction = %q", got)
	}
	if got := direction("360"); got != "북" {
		t.Fatalf("north direction = %q", got)
	}
}
