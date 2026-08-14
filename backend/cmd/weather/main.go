package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"ndweather/backend/internal/config"
	"ndweather/backend/internal/httpapi"
	"ndweather/backend/internal/ingest"
	"ndweather/backend/internal/kma"
	"ndweather/backend/internal/store"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	c, e := config.Load()
	if e != nil {
		slog.Error("configuration invalid", "error", e)
		os.Exit(1)
	}
	for _, d := range []string{c.DataDir, c.AssetDir, c.InboxDir, filepath.Join(c.DataDir, "quarantine")} {
		if e = os.MkdirAll(d, 0750); e != nil {
			slog.Error("directory unavailable", "error", e)
			os.Exit(1)
		}
	}
	s, e := store.Open(c.DBPath, c.SitesPath)
	if e != nil {
		slog.Error("database initialization failed", "error", e)
		os.Exit(1)
	}
	defer s.DB.Close()
	svc := &ingest.Service{Store: s, AssetDir: c.AssetDir, MaxAssetBytes: c.MaxAssetMB << 20}
	if c.Demo {
		seed(svc)
	}
	_ = s.Cleanup(time.Now(), c.RetentionDays, c.RadarRetentionHours)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cleanup(ctx, s, c)
	if c.KMAServiceKey != "" {
		go collectKMA(ctx, c, s, svc)
	}
	if c.IngestMode == "directory" {
		go inbox(ctx, c, svc)
	}
	api := &httpapi.API{C: c, S: s, I: svc, Static: httpapi.StaticHandler()}
	server := &http.Server{Addr: c.Bind + ":" + itoa(c.Port), Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		slog.Info("server started", "address", server.Addr, "mode", c.IngestMode)
		if e := server.ListenAndServe(); !errors.Is(e, http.ErrServerClosed) {
			slog.Error("server stopped", "error", e)
			os.Exit(1)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	cancel()
	x, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	_ = server.Shutdown(x)
	slog.Info("shutdown complete")
}
func collectKMA(ctx context.Context, c config.Config, s *store.Store, svc *ingest.Service) {
	loc, _ := time.LoadLocation(c.Timezone)
	client := &kma.Client{Endpoint: c.KMAEndpoint, ServiceKey: c.KMAServiceKey, HTTP: &http.Client{Timeout: 20 * time.Second}, Location: loc}
	collect := func() {
		sites, e := s.Sites()
		if e != nil {
			slog.Error("KMA sites unavailable", "error", e)
			return
		}
		for _, site := range sites {
			batch, e := client.Fetch(ctx, site, time.Now())
			if e == nil {
				_, e = svc.Process(ctx, batch)
			}
			if e != nil {
				slog.Error("KMA collection failed", "site", site.Code, "error", e)
			}
		}
	}
	collect()
	ticker := time.NewTicker(time.Duration(c.KMAPollSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}
func itoa(n int) string {
	const ds = "0123456789"
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 8)
	for n > 0 {
		b = append([]byte{ds[n%10]}, b...)
		n /= 10
	}
	return string(b)
}
func cleanup(ctx context.Context, s *store.Store, c config.Config) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			_ = s.Cleanup(now, c.RetentionDays, c.RadarRetentionHours)
		}
	}
}
func inbox(ctx context.Context, c config.Config, svc *ingest.Service) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			files, _ := filepath.Glob(filepath.Join(c.InboxDir, "*.json"))
			for _, p := range files {
				b, e := os.ReadFile(p)
				if e == nil {
					var x ingest.Batch
					x, e = ingest.DecodeJSON(b)
					if e == nil {
						_, e = svc.Process(ctx, x)
					}
				}
				if e == nil {
					_ = os.Remove(p)
				} else {
					_ = os.Rename(p, filepath.Join(c.DataDir, "quarantine", filepath.Base(p)))
				}
			}
		}
	}
}
func seed(s *ingest.Service) {
	now := time.Now().UTC().Truncate(time.Second)
	b := ingest.Batch{SchemaVersion: "1.0", BatchID: "DEMO-" + now.Format("20060102-150405"), Source: "DEMO-DATA", CreatedAt: now.Format(time.RFC3339)}
	temp, hum, wind, gust, rain := 23.4, 66.0, 3.2, 6.1, 0.0
	b.Records.Observations = []ingest.Observation{
		{SiteCode: "SAMPLE", ObservedAt: now.Format(time.RFC3339), Temperature: &temp, Humidity: &hum, WindDirection: "남서", WindSpeed: &wind, GustSpeed: &gust, Precipitation: &rain, PrecipitationState: "없음", Sky: "구름조금"},
		{SiteCode: "SOUTH", ObservedAt: now.Format(time.RFC3339), Temperature: &temp, Humidity: &hum, WindDirection: "남", WindSpeed: &wind, Sky: "맑음"},
	}
	min, max, pop := 19.0, 28.0, 30
	b.Records.Forecasts = []ingest.Forecast{{SiteCode: "SAMPLE", IssuedAt: now.Format(time.RFC3339), ValidAt: now.Add(time.Hour).Format(time.RFC3339), MinTemperature: &min, MaxTemperature: &max, RainProbability: &pop, Sky: "구름많음"}}
	b.Records.Warnings = []ingest.Warning{{WarningID: "DEMO-WARNING", Phenomenon: "강풍", Level: "주의보", AreaCode: "SAMPLE-AREA", AreaName: "샘플지역", AnnouncedAt: now.Format(time.RFC3339), EffectiveAt: now.Format(time.RFC3339), SiteCodes: []string{"SAMPLE"}}}
	pressure, maxWind, speed := 980, 32.0, 18.0
	b.Records.Typhoons = []ingest.Typhoon{{Key: "DEMO-TYPHOON", Number: "99", Name: "데모", Latitude: 25, Longitude: 130, Pressure: &pressure, MaxWind: &maxWind, Direction: "북", Speed: &speed, AnnouncedAt: now.Format(time.RFC3339), Active: true}}
	_, _ = s.Process(context.Background(), b)
	_, _ = json.Marshal(b)
}
