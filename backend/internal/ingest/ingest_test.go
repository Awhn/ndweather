package ingest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ndweather/backend/internal/store"
)

func testService(t *testing.T) Service {
	t.Helper()
	dir := t.TempDir()
	sites := filepath.Join(dir, "sites.yaml")
	if err := os.WriteFile(sites, []byte("- code: SAMPLE\n  name: Sample\n  latitude: 1\n  longitude: 1\n  forecastGridX: 1\n  forecastGridY: 1\n  warningAreaCodes: []\n  displayOrder: 1\n  enabled: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(dir, "w.db"), sites)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DB.Close() })
	return Service{Store: s, AssetDir: filepath.Join(dir, "assets"), MaxAssetBytes: 100}
}

func testAsset(id string, body []byte) Asset {
	sum := sha256.Sum256(body)
	return Asset{AssetID: id, AssetType: "radar", FileName: id + ".png", ContentType: "image/png", ObservedAt: time.Now().UTC().Format(time.RFC3339), Encoding: "base64", OriginalSize: len(body), SHA256: hex.EncodeToString(sum[:]), Payload: base64.StdEncoding.EncodeToString(body)}
}

func TestUtilities(t *testing.T) {
	if _, e := ParseTime("bad"); e == nil {
		t.Fatal("bad time accepted")
	}
	png := []byte("\x89PNG\r\n\x1a\nrest")
	a := testAsset("RADAR-1", png)
	if _, e := DecodeAsset(a, 100); e != nil {
		t.Fatal(e)
	}
	a.SHA256 = "bad"
	if _, e := DecodeAsset(a, 100); e == nil {
		t.Fatal("bad digest accepted")
	}
	if filepath.Ext(SafeName("../../x", "image/png")) != ".png" {
		t.Fatal("unsafe name")
	}
	now := time.Now()
	if IsStale(now, now, 20) != "normal" || IsStale(now.Add(-21*time.Minute), now, 20) != "stale" {
		t.Fatal("freshness")
	}
	if !RetentionCutoff(now, 30).Before(now) {
		t.Fatal("retention")
	}
}

func TestProcessIdempotentAndRollback(t *testing.T) {
	svc := testService(t)
	now := time.Now().UTC().Format(time.RFC3339)
	temp := 2.0
	b := Batch{SchemaVersion: "1.0", BatchID: "WEATHER-TEST-0001", Source: "test", CreatedAt: now}
	b.Records.Observations = []Observation{{SiteCode: "SAMPLE", ObservedAt: now, Temperature: &temp}}
	r, err := svc.Process(context.Background(), b)
	if err != nil || r.Observations != 1 {
		t.Fatal(err)
	}
	r, err = svc.Process(context.Background(), b)
	if err != nil || !r.Duplicate {
		t.Fatal("not idempotent")
	}
	bad := b
	bad.BatchID = "WEATHER-TEST-0002"
	bad.Records.Observations[0].SiteCode = "MISSING"
	if _, err = svc.Process(context.Background(), bad); err == nil {
		t.Fatal("expected FK rollback")
	}
	var n int
	_ = svc.Store.DB.QueryRow("SELECT count(*) FROM ingest_batches").Scan(&n)
	if n != 1 {
		t.Fatalf("rollback failed %d", n)
	}
}

func TestProcessUpdatesSnapshotsAndRelationships(t *testing.T) {
	svc := testService(t)
	now := time.Now().UTC().Truncate(time.Second)
	warning := Warning{WarningID: "APIHUB-SAMPLE-W", Phenomenon: "강풍", Level: "주의보", AreaCode: "AREA", AreaName: "Area", AnnouncedAt: now.Format(time.RFC3339), EffectiveAt: now.Format(time.RFC3339), SiteCodes: []string{"SAMPLE"}}
	warningBatch := Batch{SchemaVersion: "1.0", BatchID: "APIHUB-WARNING-20260815-1000", Source: "KMA-APIHub", CreatedAt: now.Format(time.RFC3339)}
	warningBatch.Records.Warnings = []Warning{warning}
	if _, err := svc.Process(context.Background(), warningBatch); err != nil {
		t.Fatal(err)
	}
	warningBatch.BatchID = "APIHUB-WARNING-20260815-1010"
	warningBatch.Records.Warnings[0].Level = "경보"
	if _, err := svc.Process(context.Background(), warningBatch); err != nil {
		t.Fatal(err)
	}
	var level string
	var siteCount int
	if err := svc.Store.DB.QueryRow(`SELECT level FROM warnings WHERE warning_id=?`, warning.WarningID).Scan(&level); err != nil || level != "경보" {
		t.Fatalf("warning update: level=%q err=%v", level, err)
	}
	_ = svc.Store.DB.QueryRow(`SELECT count(*) FROM warning_sites`).Scan(&siteCount)
	if siteCount != 1 {
		t.Fatalf("warning site relationships = %d", siteCount)
	}
	warningBatch.BatchID = "APIHUB-WARNING-20260815-1020"
	warningBatch.Records.Warnings = nil
	if _, err := svc.Process(context.Background(), warningBatch); err != nil {
		t.Fatal(err)
	}
	var expires sql.NullString
	_ = svc.Store.DB.QueryRow(`SELECT expires_at FROM warnings WHERE warning_id=?`, warning.WarningID).Scan(&expires)
	if !expires.Valid {
		t.Fatal("warning snapshot did not expire missing warning")
	}

	pressure := 980
	wind := 30.0
	typhoon := Typhoon{Key: "APIHUB-2026-1", Number: "1", Name: "TEST", Latitude: 20, Longitude: 130, Pressure: &pressure, MaxWind: &wind, AnnouncedAt: now.Format(time.RFC3339), Active: true, ForecastPoints: []Point{{ForecastAt: now.Add(time.Hour).Format(time.RFC3339), Latitude: 21, Longitude: 129}}}
	typhoonBatch := Batch{SchemaVersion: "1.0", BatchID: "APIHUB-TYPHOON-20260815-1000", Source: "KMA-APIHub", CreatedAt: now.Format(time.RFC3339)}
	typhoonBatch.Records.Typhoons = []Typhoon{typhoon}
	if _, err := svc.Process(context.Background(), typhoonBatch); err != nil {
		t.Fatal(err)
	}
	typhoonBatch.BatchID = "APIHUB-TYPHOON-20260815-1010"
	typhoonBatch.Records.Typhoons[0].Latitude = 22
	typhoonBatch.Records.Typhoons[0].ForecastPoints[0].Latitude = 23
	if _, err := svc.Process(context.Background(), typhoonBatch); err != nil {
		t.Fatal(err)
	}
	var latitude float64
	var points int
	_ = svc.Store.DB.QueryRow(`SELECT latitude FROM typhoons WHERE typhoon_key=?`, typhoon.Key).Scan(&latitude)
	_ = svc.Store.DB.QueryRow(`SELECT count(*) FROM typhoon_forecast_points`).Scan(&points)
	if latitude != 22 || points != 1 {
		t.Fatalf("typhoon update: latitude=%v points=%d", latitude, points)
	}
	typhoonBatch.BatchID = "APIHUB-TYPHOON-20260815-1020"
	typhoonBatch.Records.Typhoons = nil
	if _, err := svc.Process(context.Background(), typhoonBatch); err != nil {
		t.Fatal(err)
	}
	var active bool
	_ = svc.Store.DB.QueryRow(`SELECT active FROM typhoons WHERE typhoon_key=?`, typhoon.Key).Scan(&active)
	if active {
		t.Fatal("typhoon snapshot did not deactivate missing typhoon")
	}
}

func TestAssetConflictDoesNotOverwriteStoredFile(t *testing.T) {
	svc := testService(t)
	now := time.Now().UTC().Format(time.RFC3339)
	first := Batch{SchemaVersion: "1.0", BatchID: "WEATHER-ASSET-0001", Source: "test", CreatedAt: now, Assets: []Asset{testAsset("RADAR-SAME", []byte("\x89PNG\r\n\x1a\nfirst"))}}
	if _, err := svc.Process(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(svc.AssetDir, SafeName("RADAR-SAME", "image/png"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.BatchID = "WEATHER-ASSET-0002"
	second.Assets = []Asset{testAsset("RADAR-SAME", []byte("\x89PNG\r\n\x1a\nsecond"))}
	if _, err = svc.Process(context.Background(), second); err == nil {
		t.Fatal("expected asset conflict")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("stored asset changed: err=%v", err)
	}
}

func TestAssetFinalizeFailureRollsBackDatabase(t *testing.T) {
	svc := testService(t)
	now := time.Now().UTC().Format(time.RFC3339)
	asset := testAsset("RADAR-BLOCKED", []byte("\x89PNG\r\n\x1a\nblocked"))
	blockedPath := filepath.Join(svc.AssetDir, SafeName(asset.AssetID, asset.ContentType))
	if err := os.MkdirAll(blockedPath, 0750); err != nil {
		t.Fatal(err)
	}
	b := Batch{SchemaVersion: "1.0", BatchID: "WEATHER-ASSET-0003", Source: "test", CreatedAt: now, Assets: []Asset{asset}}
	if _, err := svc.Process(context.Background(), b); err == nil {
		t.Fatal("expected asset finalization failure")
	}
	var batches, assets int
	_ = svc.Store.DB.QueryRow(`SELECT count(*) FROM ingest_batches`).Scan(&batches)
	_ = svc.Store.DB.QueryRow(`SELECT count(*) FROM radar_assets`).Scan(&assets)
	if batches != 0 || assets != 0 {
		t.Fatalf("database was committed: batches=%d assets=%d", batches, assets)
	}
}

func TestDecodeJSONRejectsTrailingDocumentAndNestedInvalidTime(t *testing.T) {
	if _, err := DecodeJSON([]byte(`{} {}`)); err == nil {
		t.Fatal("accepted trailing JSON document")
	}
	svc := testService(t)
	now := time.Now().UTC().Format(time.RFC3339)
	bad := "not-a-time"
	b := Batch{SchemaVersion: "1.0", BatchID: "WEATHER-TIME-0001", Source: "test", CreatedAt: now}
	b.Records.Warnings = []Warning{{WarningID: "W", AnnouncedAt: now, EffectiveAt: now, ExpiresAt: &bad}}
	if _, err := svc.Process(context.Background(), b); err == nil {
		t.Fatal("accepted invalid warning expiry")
	}
}
