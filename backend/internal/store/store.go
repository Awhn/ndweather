package store

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

//go:embed 001_init.sql
var migration string

type Site struct {
	Code      string   `yaml:"code" json:"code"`
	Name      string   `yaml:"name" json:"name"`
	Latitude  float64  `yaml:"latitude" json:"latitude"`
	Longitude float64  `yaml:"longitude" json:"longitude"`
	GridX     int      `yaml:"forecastGridX" json:"forecastGridX"`
	GridY     int      `yaml:"forecastGridY" json:"forecastGridY"`
	Areas     []string `yaml:"warningAreaCodes" json:"warningAreaCodes"`
	Order     int      `yaml:"displayOrder" json:"displayOrder"`
	Enabled   bool     `yaml:"enabled" json:"enabled"`
}
type Store struct{ DB *sql.DB }

func Open(path, sitesPath string) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0750); e != nil {
		return nil, e
	}
	absPath, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	q := url.Values{}
	q.Add("_pragma", "foreign_keys=ON")
	q.Add("_pragma", "busy_timeout=5000")
	q.Add("_pragma", "journal_mode=WAL")
	q.Add("_pragma", "synchronous=NORMAL")
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absPath), RawQuery: q.Encode()}).String()
	db, e := sql.Open("sqlite", dsn)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db}
	if e = s.init(sitesPath); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) init(sitesPath string) error {
	for _, p := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, e := s.DB.Exec(p); e != nil {
			return e
		}
	}
	if _, e := s.DB.Exec(migration); e != nil {
		return e
	}
	b, e := os.ReadFile(sitesPath)
	if e != nil {
		return e
	}
	var sites []Site
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	decoder.KnownFields(true)
	if e = decoder.Decode(&sites); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, site := range sites {
		if site.Code == "" || site.Name == "" || site.Latitude < -90 || site.Latitude > 90 || site.Longitude < -180 || site.Longitude > 180 || site.GridX <= 0 || site.GridY <= 0 {
			return fmt.Errorf("invalid site configuration for %q", site.Code)
		}
		if seen[site.Code] {
			return fmt.Errorf("duplicate site code %q", site.Code)
		}
		seen[site.Code] = true
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE sites SET enabled=0`); e != nil {
		return e
	}
	for _, x := range sites {
		a, marshalErr := json.Marshal(x.Areas)
		if marshalErr != nil {
			return marshalErr
		}
		_, e = tx.Exec(`INSERT INTO sites VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(code) DO UPDATE SET name=excluded.name,latitude=excluded.latitude,longitude=excluded.longitude,forecast_grid_x=excluded.forecast_grid_x,forecast_grid_y=excluded.forecast_grid_y,warning_area_codes=excluded.warning_area_codes,display_order=excluded.display_order,enabled=excluded.enabled`, x.Code, x.Name, x.Latitude, x.Longitude, x.GridX, x.GridY, string(a), x.Order, x.Enabled)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Ready(ctx context.Context) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, "INSERT INTO readiness_probe(id,checked_at) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET checked_at=excluded.checked_at", time.Now().UTC().Format(time.RFC3339))
	return e
}
func (s *Store) RecordCollector(name string, collectionErr error) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var success any
	var message any
	if collectionErr == nil {
		success = now
	} else {
		message = collectionErr.Error()
	}
	_, err := s.DB.Exec(`INSERT INTO collector_status(name,last_attempt_at,last_success_at,last_error) VALUES(?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET last_attempt_at=excluded.last_attempt_at,
		last_success_at=CASE WHEN excluded.last_success_at IS NOT NULL THEN excluded.last_success_at ELSE collector_status.last_success_at END,
		last_error=excluded.last_error`, name, now, success, message)
	return err
}
func (s *Store) Sites() ([]Site, error) {
	rows, e := s.DB.Query(`SELECT code,name,latitude,longitude,forecast_grid_x,forecast_grid_y,warning_area_codes,display_order,enabled FROM sites WHERE enabled=1 ORDER BY display_order`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		var x Site
		var a string
		if e = rows.Scan(&x.Code, &x.Name, &x.Latitude, &x.Longitude, &x.GridX, &x.GridY, &a, &x.Order, &x.Enabled); e != nil {
			return nil, e
		}
		json.Unmarshal([]byte(a), &x.Areas)
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) Cleanup(now time.Time, days, hours int) error {
	history := now.AddDate(0, 0, -days).UTC().Format(time.RFC3339)
	radar := now.Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, t := range []string{"observations", "forecasts", "warnings", "typhoons"} {
		if _, e = tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE received_at < ?", t), history); e != nil {
			return e
		}
	}
	rows, e := tx.Query("SELECT path FROM radar_assets WHERE received_at < ? AND id NOT IN (SELECT id FROM radar_assets ORDER BY observed_at DESC LIMIT 1)", radar)
	if e != nil {
		return e
	}
	var paths []string
	for rows.Next() {
		var p string
		if e = rows.Scan(&p); e != nil {
			rows.Close()
			return e
		}
		paths = append(paths, p)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return e
	}
	if e = rows.Close(); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM radar_assets WHERE received_at < ? AND id NOT IN (SELECT id FROM radar_assets ORDER BY observed_at DESC LIMIT 1)", radar); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	for _, p := range paths {
		_ = os.Remove(p)
	}
	return nil
}
