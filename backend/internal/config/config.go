package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
	_ "time/tzdata"
)

type Config struct {
	AppEnv, Bind, DataDir, DBPath, AssetDir, InboxDir, IngestMode, IngestToken, ReadToken, Timezone, SitesPath, KMAServiceKey, KMAEndpoint        string
	Port, RotateSeconds, RefreshSeconds, RadarSeconds, StaleMinutes, RetentionDays, RadarRetentionHours, MaxRequestMB, MaxAssetMB, KMAPollSeconds int
	Demo                                                                                                                                          bool
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func integer(k string, d int) (int, error) {
	v := env(k, strconv.Itoa(d))
	n, e := strconv.Atoi(v)
	if e != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", k)
	}
	return n, nil
}
func Load() (Config, error) {
	c := Config{AppEnv: env("APP_ENV", "development"), Bind: env("BIND_ADDRESS", "0.0.0.0"), DataDir: env("DATA_DIR", "./data"), IngestMode: env("INGEST_MODE", "http"), IngestToken: os.Getenv("INGEST_TOKEN"), ReadToken: os.Getenv("READ_API_TOKEN"), Timezone: env("TIMEZONE", "Asia/Seoul"), SitesPath: env("SITES_CONFIG", "config/sites.yaml"), KMAServiceKey: os.Getenv("KMA_SERVICE_KEY"), KMAEndpoint: env("KMA_ENDPOINT", "https://apis.data.go.kr/1360000/VilageFcstInfoService_2.0"), Demo: env("DEMO_MODE", "false") == "true"}
	var e error
	for _, x := range []struct {
		k string
		d int
		p *int
	}{{"PORT", 8080, &c.Port}, {"DISPLAY_ROTATE_SECONDS", 5, &c.RotateSeconds}, {"DISPLAY_REFRESH_SECONDS", 30, &c.RefreshSeconds}, {"RADAR_FRAME_SECONDS", 1, &c.RadarSeconds}, {"DATA_STALE_MINUTES", 20, &c.StaleMinutes}, {"RETENTION_DAYS", 30, &c.RetentionDays}, {"RADAR_RETENTION_HOURS", 72, &c.RadarRetentionHours}, {"MAX_REQUEST_MB", 25, &c.MaxRequestMB}, {"MAX_ASSET_MB", 20, &c.MaxAssetMB}, {"KMA_POLL_SECONDS", 3600, &c.KMAPollSeconds}} {
		*x.p, e = integer(x.k, x.d)
		if e != nil {
			return c, e
		}
	}
	c.DBPath = env("SQLITE_PATH", filepath.Join(c.DataDir, "weather.db"))
	c.AssetDir = env("ASSET_DIR", filepath.Join(c.DataDir, "assets"))
	c.InboxDir = env("INBOX_DIR", filepath.Join(c.DataDir, "inbox"))
	if c.IngestMode != "http" && c.IngestMode != "directory" {
		return c, fmt.Errorf("INGEST_MODE must be http or directory")
	}
	if c.IngestMode == "http" && len(c.IngestToken) < 16 {
		return c, fmt.Errorf("INGEST_TOKEN must contain at least 16 characters in http mode")
	}
	if _, e = time.LoadLocation(c.Timezone); e != nil {
		return c, fmt.Errorf("invalid TIMEZONE: %w", e)
	}
	return c, nil
}
