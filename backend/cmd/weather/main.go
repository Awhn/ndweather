package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"ndweather/backend/internal/apihub"
	"ndweather/backend/internal/config"
	"ndweather/backend/internal/httpapi"
	"ndweather/backend/internal/ingest"
	"ndweather/backend/internal/kma"
	"ndweather/backend/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	c, e := config.Load()
	if e != nil {
		slog.Error("configuration invalid", "error", e)
		os.Exit(1)
	}
	levels := map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: levels[c.LogLevel]})))
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
		if e = seed(svc); e != nil {
			slog.Error("demo data initialization failed", "error", e)
			os.Exit(1)
		}
	}
	if e = s.Cleanup(time.Now(), c.RetentionDays, c.RadarRetentionHours); e != nil {
		slog.Error("initial cleanup failed", "error", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cleanup(ctx, s, c)
	if c.KMAServiceKey != "" {
		go collectKMA(ctx, c, s, svc)
	}
	if c.KMAAPIHubKey != "" {
		go collectAPIHub(ctx, c, s, svc)
	}
	if c.IngestMode == "directory" {
		go inbox(ctx, c, svc)
	}
	api := &httpapi.API{C: c, S: s, I: svc, Static: httpapi.StaticHandler()}
	server := &http.Server{Addr: net.JoinHostPort(c.Bind, strconv.Itoa(c.Port)), Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
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
	if e = server.Shutdown(x); e != nil {
		slog.Error("graceful shutdown failed", "error", e)
	}
	slog.Info("shutdown complete")
}
func collectAPIHub(ctx context.Context, c config.Config, s *store.Store, svc *ingest.Service) {
	loc, _ := time.LoadLocation(c.Timezone)
	client := &apihub.Client{Endpoint: c.KMAAPIHubEndpoint, AuthKey: c.KMAAPIHubKey, HTTP: &http.Client{Timeout: 30 * time.Second}, Location: loc}
	collect := func() {
		now := time.Now()
		sites, e := s.Sites()
		if e != nil {
			slog.Error("API Hub sites unavailable", "error", e)
			return
		}
		fetches := []struct {
			name  string
			fetch func() (ingest.Batch, error)
		}{{"radar", func() (ingest.Batch, error) { return client.FetchRadar(ctx, now) }}, {"warnings", func() (ingest.Batch, error) { return client.FetchWarnings(ctx, sites, now) }}, {"typhoons", func() (ingest.Batch, error) { return client.FetchTyphoons(ctx, now) }}}
		for _, f := range fetches {
			batch, e := f.fetch()
			if e == nil {
				_, e = svc.Process(ctx, batch)
			}
			if statusErr := s.RecordCollector("apihub_"+f.name, e); statusErr != nil {
				slog.Error("API Hub status update failed", "product", f.name, "error", statusErr)
			}
			if e != nil {
				slog.Error("API Hub collection failed", "product", f.name, "error", e)
			} else {
				slog.Info("API Hub collection completed", "product", f.name)
			}
		}
	}
	collect()
	ticker := time.NewTicker(time.Duration(c.KMAAPIHubPollSeconds) * time.Second)
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
func collectKMA(ctx context.Context, c config.Config, s *store.Store, svc *ingest.Service) {
	loc, _ := time.LoadLocation(c.Timezone)
	client := &kma.Client{Endpoint: c.KMAEndpoint, ServiceKey: c.KMAServiceKey, HTTP: &http.Client{Timeout: 20 * time.Second}, Location: loc}
	collect := func() {
		sites, e := s.Sites()
		if e != nil {
			slog.Error("KMA sites unavailable", "error", e)
			_ = s.RecordCollector("kma_forecast", e)
			return
		}
		if len(sites) == 0 {
			e = errors.New("no enabled sites")
			slog.Error("KMA collection failed", "error", e)
			_ = s.RecordCollector("kma_forecast", e)
			return
		}
		var collectionErrors []error
		for _, site := range sites {
			batch, e := client.Fetch(ctx, site, time.Now())
			if e == nil {
				_, e = svc.Process(ctx, batch)
			}
			if e != nil {
				slog.Error("KMA collection failed", "site", site.Code, "error", e)
				collectionErrors = append(collectionErrors, e)
			}
		}
		e = errors.Join(collectionErrors...)
		if statusErr := s.RecordCollector("kma_forecast", e); statusErr != nil {
			slog.Error("KMA status update failed", "error", statusErr)
		}
		if e == nil {
			slog.Info("KMA collection completed", "sites", len(sites))
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
func cleanup(ctx context.Context, s *store.Store, c config.Config) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if e := s.Cleanup(now, c.RetentionDays, c.RadarRetentionHours); e != nil {
				slog.Error("scheduled cleanup failed", "error", e)
			}
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
			files, e := filepath.Glob(filepath.Join(c.InboxDir, "*.json"))
			if e != nil {
				slog.Error("inbox scan failed", "error", e)
				continue
			}
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
					if e = os.Remove(p); e != nil {
						slog.Error("processed inbox file removal failed", "path", p, "error", e)
					}
				} else {
					destination := filepath.Join(c.DataDir, "quarantine", filepath.Base(p)+"-"+strconv.FormatInt(time.Now().UnixNano(), 10))
					if renameErr := os.Rename(p, destination); renameErr != nil {
						slog.Error("inbox quarantine failed", "path", p, "error", renameErr)
					} else {
						slog.Error("inbox batch rejected", "path", destination, "error", e)
					}
				}
			}
		}
	}
}
func seed(s *ingest.Service) error {
	now := time.Now().UTC().Truncate(time.Second)
	b := ingest.Batch{SchemaVersion: "1.0", BatchID: "DEMO-" + now.Format("20060102-150405"), Source: "DEMO-DATA", CreatedAt: now.Format(time.RFC3339)}
	sites, err := s.Store.Sites()
	if err != nil {
		return err
	}
	if len(sites) == 0 {
		return errors.New("demo mode requires at least one enabled site")
	}
	temp, hum, wind, gust, rain := 23.4, 66.0, 3.2, 6.1, 0.0
	min, max, pop := 19.0, 28.0, 30
	for _, site := range sites {
		b.Records.Observations = append(b.Records.Observations, ingest.Observation{SiteCode: site.Code, ObservedAt: now.Format(time.RFC3339), Temperature: &temp, Humidity: &hum, WindDirection: "남서", WindSpeed: &wind, GustSpeed: &gust, Precipitation: &rain, PrecipitationState: "없음", Sky: "구름조금"})
		b.Records.Forecasts = append(b.Records.Forecasts, ingest.Forecast{SiteCode: site.Code, IssuedAt: now.Format(time.RFC3339), ValidAt: now.Add(time.Hour).Format(time.RFC3339), MinTemperature: &min, MaxTemperature: &max, RainProbability: &pop, Sky: "구름많음"})
	}
	b.Records.Warnings = []ingest.Warning{{WarningID: "DEMO-WARNING", Phenomenon: "강풍", Level: "주의보", AreaCode: "DEMO-AREA", AreaName: sites[0].Name, AnnouncedAt: now.Format(time.RFC3339), EffectiveAt: now.Format(time.RFC3339), SiteCodes: []string{sites[0].Code}}}
	pressure, maxWind, speed := 980, 32.0, 18.0
	b.Records.Typhoons = []ingest.Typhoon{{Key: "DEMO-TYPHOON", Number: "99", Name: "데모", Latitude: 25, Longitude: 130, Pressure: &pressure, MaxWind: &maxWind, Direction: "북", Speed: &speed, AnnouncedAt: now.Format(time.RFC3339), Active: true}}
	_, err = s.Process(context.Background(), b)
	return err
}
