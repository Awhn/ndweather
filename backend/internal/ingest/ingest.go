package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ndweather/backend/internal/store"
)

var batchRE = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{7,127}$`)

type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

type Observation struct {
	SiteCode           string   `json:"siteCode"`
	ObservedAt         string   `json:"observedAt"`
	Temperature        *float64 `json:"temperature"`
	Humidity           *float64 `json:"humidity"`
	WindDirection      string   `json:"windDirection"`
	WindSpeed          *float64 `json:"windSpeed"`
	GustSpeed          *float64 `json:"gustSpeed"`
	Precipitation      *float64 `json:"precipitation"`
	PrecipitationState string   `json:"precipitationState"`
	Sky                string   `json:"sky"`
}
type Forecast struct {
	SiteCode        string   `json:"siteCode"`
	IssuedAt        string   `json:"issuedAt"`
	ValidAt         string   `json:"validAt"`
	MinTemperature  *float64 `json:"minTemperature"`
	MaxTemperature  *float64 `json:"maxTemperature"`
	RainProbability *int     `json:"rainProbability"`
	Sky             string   `json:"sky"`
}
type Warning struct {
	WarningID   string   `json:"warningId"`
	Phenomenon  string   `json:"phenomenon"`
	Level       string   `json:"level"`
	AreaCode    string   `json:"areaCode"`
	AreaName    string   `json:"areaName"`
	AnnouncedAt string   `json:"announcedAt"`
	EffectiveAt string   `json:"effectiveAt"`
	ExpiresAt   *string  `json:"expiresAt"`
	SiteCodes   []string `json:"siteCodes"`
}
type Point struct {
	ForecastAt string   `json:"forecastAt"`
	Latitude   float64  `json:"latitude"`
	Longitude  float64  `json:"longitude"`
	Pressure   *int     `json:"pressure"`
	MaxWind    *float64 `json:"maxWind"`
}
type Typhoon struct {
	Key            string   `json:"typhoonKey"`
	Number         string   `json:"number"`
	Name           string   `json:"name"`
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	Pressure       *int     `json:"pressure"`
	MaxWind        *float64 `json:"maxWind"`
	Direction      string   `json:"direction"`
	Speed          *float64 `json:"speed"`
	AnnouncedAt    string   `json:"announcedAt"`
	Active         bool     `json:"active"`
	ForecastPoints []Point  `json:"forecastPoints"`
}
type Asset struct {
	AssetID      string `json:"assetId"`
	AssetType    string `json:"assetType"`
	FileName     string `json:"fileName"`
	ContentType  string `json:"contentType"`
	ObservedAt   string `json:"observedAt"`
	Encoding     string `json:"encoding"`
	OriginalSize int    `json:"originalSize"`
	SHA256       string `json:"sha256"`
	Payload      string `json:"payload"`
}
type Batch struct {
	SchemaVersion string `json:"schemaVersion"`
	BatchID       string `json:"batchId"`
	Source        string `json:"source"`
	CreatedAt     string `json:"createdAt"`
	Records       struct {
		Observations []Observation `json:"observations"`
		Forecasts    []Forecast    `json:"forecasts"`
		Warnings     []Warning     `json:"warnings"`
		Typhoons     []Typhoon     `json:"typhoons"`
	} `json:"records"`
	Assets []Asset `json:"assets"`
}
type Service struct {
	Store         *store.Store
	AssetDir      string
	MaxAssetBytes int
}
type Result struct {
	BatchID      string `json:"batchId"`
	Duplicate    bool   `json:"duplicate"`
	Observations int    `json:"observations"`
	Forecasts    int    `json:"forecasts"`
	Warnings     int    `json:"warnings"`
	Typhoons     int    `json:"typhoons"`
	Assets       int    `json:"assets"`
}

func ParseTime(v string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339, v)
	if e != nil {
		return t, &Error{"INVALID_TIME", "date must be ISO 8601/RFC3339"}
	}
	return t, nil
}
func SafeName(id, mime string) string {
	sum := sha256.Sum256([]byte(id))
	ext := ".bin"
	if mime == "image/png" {
		ext = ".png"
	} else if mime == "image/jpeg" {
		ext = ".jpg"
	}
	return hex.EncodeToString(sum[:16]) + ext
}
func DecodeAsset(a Asset, max int) ([]byte, error) {
	if a.Encoding != "base64" {
		return nil, &Error{"INVALID_ENCODING", "encoding must be base64"}
	}
	if a.ContentType != "image/png" && a.ContentType != "image/jpeg" {
		return nil, &Error{"UNSUPPORTED_MIME", "asset MIME is not allowed"}
	}
	b, e := base64.StdEncoding.DecodeString(a.Payload)
	if e != nil {
		return nil, &Error{"INVALID_BASE64", "payload is not valid base64"}
	}
	if len(b) > max {
		return nil, &Error{"ASSET_TOO_LARGE", "asset exceeds configured limit"}
	}
	if len(b) != a.OriginalSize {
		return nil, &Error{"SIZE_MISMATCH", "originalSize mismatch"}
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != a.SHA256 {
		return nil, &Error{"SHA256_MISMATCH", "sha256 mismatch"}
	}
	if a.ContentType == "image/png" && (len(b) < 8 || string(b[:8]) != "\x89PNG\r\n\x1a\n") {
		return nil, &Error{"MAGIC_MISMATCH", "invalid PNG signature"}
	}
	if a.ContentType == "image/jpeg" && (len(b) < 3 || b[0] != 0xff || b[1] != 0xd8 || b[2] != 0xff) {
		return nil, &Error{"MAGIC_MISMATCH", "invalid JPEG signature"}
	}
	return b, nil
}
func validTime(v string) error { _, e := ParseTime(v); return e }
func (s *Service) Process(ctx context.Context, b Batch) (Result, error) {
	r := Result{BatchID: b.BatchID}
	if b.SchemaVersion != "1.0" {
		return r, &Error{"UNSUPPORTED_SCHEMA", "schemaVersion must be 1.0"}
	}
	if !batchRE.MatchString(b.BatchID) {
		return r, &Error{"INVALID_BATCH_ID", "invalid batchId format"}
	}
	if b.Source == "" || validTime(b.CreatedAt) != nil {
		return r, &Error{"INVALID_BATCH", "source and createdAt are required"}
	}
	type decoded struct {
		a       Asset
		b       []byte
		path    string
		tmp     string
		publish bool
	}
	ds := make([]decoded, 0, len(b.Assets))
	if len(b.Assets) > 20 {
		return r, &Error{"TOO_MANY_ASSETS", "maximum 20 assets"}
	}
	assetIDs := map[string]bool{}
	assetHashes := map[string]bool{}
	for _, a := range b.Assets {
		if a.AssetType != "radar" || a.AssetID == "" || validTime(a.ObservedAt) != nil {
			return r, &Error{"INVALID_ASSET", "invalid asset metadata"}
		}
		if assetIDs[a.AssetID] || assetHashes[a.SHA256] {
			return r, &Error{"DUPLICATE_ASSET", "assetId and sha256 must be unique within a batch"}
		}
		assetIDs[a.AssetID] = true
		assetHashes[a.SHA256] = true
		d, e := DecodeAsset(a, s.MaxAssetBytes)
		if e != nil {
			return r, e
		}
		path := filepath.Join(s.AssetDir, SafeName(a.AssetID, a.ContentType))
		ds = append(ds, decoded{a: a, b: d, path: path})
	}
	for _, o := range b.Records.Observations {
		if o.SiteCode == "" || validTime(o.ObservedAt) != nil {
			return r, &Error{"INVALID_OBSERVATION", "invalid observation"}
		}
	}
	for _, f := range b.Records.Forecasts {
		if f.SiteCode == "" || validTime(f.IssuedAt) != nil || validTime(f.ValidAt) != nil {
			return r, &Error{"INVALID_FORECAST", "invalid forecast"}
		}
	}
	for _, w := range b.Records.Warnings {
		if w.WarningID == "" || validTime(w.AnnouncedAt) != nil || validTime(w.EffectiveAt) != nil {
			return r, &Error{"INVALID_WARNING", "invalid warning"}
		}
		if w.ExpiresAt != nil && validTime(*w.ExpiresAt) != nil {
			return r, &Error{"INVALID_WARNING", "invalid warning expiry"}
		}
	}
	for _, t := range b.Records.Typhoons {
		if t.Key == "" || validTime(t.AnnouncedAt) != nil {
			return r, &Error{"INVALID_TYPHOON", "invalid typhoon"}
		}
		for _, p := range t.ForecastPoints {
			if validTime(p.ForecastAt) != nil {
				return r, &Error{"INVALID_TYPHOON", "invalid typhoon forecast time"}
			}
		}
	}
	if e := os.MkdirAll(s.AssetDir, 0750); e != nil {
		return r, e
	}
	for i := range ds {
		f, e := os.CreateTemp(s.AssetDir, ".upload-*")
		if e != nil {
			return r, e
		}
		ds[i].tmp = f.Name()
		if e = f.Chmod(0640); e == nil {
			_, e = f.Write(ds[i].b)
		}
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return r, e
		}
	}
	defer func() {
		for _, d := range ds {
			if d.tmp != "" {
				_ = os.Remove(d.tmp)
			}
		}
	}()
	now := time.Now().UTC().Format(time.RFC3339)
	tx, e := s.Store.DB.BeginTx(ctx, nil)
	if e != nil {
		return r, e
	}
	defer tx.Rollback()
	var old string
	if e = tx.QueryRowContext(ctx, "SELECT result_json FROM ingest_batches WHERE batch_id=?", b.BatchID).Scan(&old); e == nil {
		if unmarshalErr := json.Unmarshal([]byte(old), &r); unmarshalErr != nil {
			return r, unmarshalErr
		}
		r.Duplicate = true
		return r, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return r, e
	}
	for _, o := range b.Records.Observations {
		var result sql.Result
		result, e = tx.Exec(`INSERT OR IGNORE INTO observations(site_code,observed_at,received_at,temperature,humidity,wind_direction,wind_speed,gust_speed,precipitation,precipitation_state,sky) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, o.SiteCode, toUTC(o.ObservedAt), now, o.Temperature, o.Humidity, o.WindDirection, o.WindSpeed, o.GustSpeed, o.Precipitation, o.PrecipitationState, o.Sky)
		if e != nil {
			return r, e
		}
		n, _ := result.RowsAffected()
		r.Observations += int(n)
	}
	for _, f := range b.Records.Forecasts {
		var result sql.Result
		result, e = tx.Exec(`INSERT OR IGNORE INTO forecasts(site_code,issued_at,valid_at,received_at,min_temperature,max_temperature,rain_probability,sky) VALUES(?,?,?,?,?,?,?,?)`, f.SiteCode, toUTC(f.IssuedAt), toUTC(f.ValidAt), now, f.MinTemperature, f.MaxTemperature, f.RainProbability, f.Sky)
		if e != nil {
			return r, e
		}
		n, _ := result.RowsAffected()
		r.Forecasts += int(n)
	}
	if b.Source == "KMA-APIHub" && strings.HasPrefix(b.BatchID, "APIHUB-WARNING-") {
		if _, e = tx.Exec(`UPDATE warnings SET expires_at=? WHERE warning_id LIKE 'APIHUB-%' AND expires_at IS NULL`, now); e != nil {
			return r, e
		}
	}
	for _, w := range b.Records.Warnings {
		var ex any
		if w.ExpiresAt != nil {
			ex = toUTC(*w.ExpiresAt)
		}
		var id int64
		e = tx.QueryRow(`INSERT INTO warnings(warning_id,phenomenon,level,area_code,area_name,announced_at,effective_at,expires_at,received_at) VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(warning_id) DO UPDATE SET phenomenon=excluded.phenomenon,level=excluded.level,area_code=excluded.area_code,area_name=excluded.area_name,announced_at=excluded.announced_at,effective_at=excluded.effective_at,expires_at=excluded.expires_at,received_at=excluded.received_at RETURNING id`, w.WarningID, w.Phenomenon, w.Level, w.AreaCode, w.AreaName, toUTC(w.AnnouncedAt), toUTC(w.EffectiveAt), ex, now).Scan(&id)
		if e != nil {
			return r, e
		}
		if _, e = tx.Exec(`DELETE FROM warning_sites WHERE warning_id=?`, id); e != nil {
			return r, e
		}
		for _, site := range w.SiteCodes {
			if _, e = tx.Exec(`INSERT INTO warning_sites VALUES(?,?)`, id, site); e != nil {
				return r, e
			}
		}
		r.Warnings++
	}
	if b.Source == "KMA-APIHub" && strings.HasPrefix(b.BatchID, "APIHUB-TYPHOON-") {
		if _, e = tx.Exec(`UPDATE typhoons SET active=0 WHERE typhoon_key LIKE 'APIHUB-%'`); e != nil {
			return r, e
		}
	}
	for _, t := range b.Records.Typhoons {
		var id int64
		e = tx.QueryRow(`INSERT INTO typhoons(typhoon_key,number,name,latitude,longitude,pressure,max_wind,direction,speed,announced_at,received_at,active) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(typhoon_key) DO UPDATE SET number=excluded.number,name=excluded.name,latitude=excluded.latitude,longitude=excluded.longitude,pressure=excluded.pressure,max_wind=excluded.max_wind,direction=excluded.direction,speed=excluded.speed,announced_at=excluded.announced_at,received_at=excluded.received_at,active=excluded.active RETURNING id`, t.Key, t.Number, t.Name, t.Latitude, t.Longitude, t.Pressure, t.MaxWind, t.Direction, t.Speed, toUTC(t.AnnouncedAt), now, t.Active).Scan(&id)
		if e != nil {
			return r, e
		}
		if _, e = tx.Exec(`DELETE FROM typhoon_forecast_points WHERE typhoon_id=?`, id); e != nil {
			return r, e
		}
		for _, p := range t.ForecastPoints {
			if _, e = tx.Exec(`INSERT INTO typhoon_forecast_points(typhoon_id,forecast_at,latitude,longitude,pressure,max_wind) VALUES(?,?,?,?,?,?)`, id, toUTC(p.ForecastAt), p.Latitude, p.Longitude, p.Pressure, p.MaxWind); e != nil {
				return r, e
			}
		}
		r.Typhoons++
	}
	for i := range ds {
		d := &ds[i]
		var existingID, existingHash string
		e = tx.QueryRow(`SELECT asset_id,sha256 FROM radar_assets WHERE asset_id=? OR sha256=? LIMIT 1`, d.a.AssetID, d.a.SHA256).Scan(&existingID, &existingHash)
		if e == nil {
			if existingID != d.a.AssetID || existingHash != d.a.SHA256 {
				return r, &Error{"ASSET_CONFLICT", "assetId or sha256 conflicts with an existing asset"}
			}
			existing, readErr := os.ReadFile(d.path)
			if errors.Is(readErr, os.ErrNotExist) {
				d.publish = true
			} else if readErr != nil {
				return r, readErr
			} else if !bytes.Equal(existing, d.b) {
				return r, &Error{"ASSET_CONFLICT", "stored asset content does not match its database digest"}
			}
			continue
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return r, e
		}
		existing, readErr := os.ReadFile(d.path)
		if errors.Is(readErr, os.ErrNotExist) {
			d.publish = true
		} else if readErr != nil {
			return r, readErr
		} else if !bytes.Equal(existing, d.b) {
			return r, &Error{"ASSET_CONFLICT", "asset path is occupied by different content"}
		}
		if _, e = tx.Exec(`INSERT INTO radar_assets(asset_id,sha256,content_type,path,observed_at,received_at,size) VALUES(?,?,?,?,?,?,?)`, d.a.AssetID, d.a.SHA256, d.a.ContentType, d.path, toUTC(d.a.ObservedAt), now, len(d.b)); e != nil {
			return r, e
		}
		r.Assets++
	}
	result, _ := json.Marshal(r)
	if _, e = tx.Exec(`INSERT INTO ingest_batches(batch_id,source,source_created_at,received_at,status,result_json) VALUES(?,?,?,?,?,?)`, b.BatchID, b.Source, toUTC(b.CreatedAt), now, "processed", string(result)); e != nil {
		return r, e
	}
	createdPaths := make([]string, 0, len(ds))
	committed := false
	defer func() {
		if !committed {
			for _, path := range createdPaths {
				_ = os.Remove(path)
			}
		}
	}()
	for i := range ds {
		d := &ds[i]
		if !d.publish {
			continue
		}
		if e = os.Link(d.tmp, d.path); e != nil {
			return r, fmt.Errorf("asset finalize failed: %w", e)
		}
		createdPaths = append(createdPaths, d.path)
	}
	if e = tx.Commit(); e != nil {
		return r, e
	}
	committed = true
	return r, nil
}
func toUTC(v string) string { t, _ := time.Parse(time.RFC3339, v); return t.UTC().Format(time.RFC3339) }
func IsStale(last time.Time, now time.Time, minutes int) string {
	if last.IsZero() {
		return "disconnected"
	}
	if now.Sub(last) > 2*time.Duration(minutes)*time.Minute {
		return "disconnected"
	}
	if now.Sub(last) > time.Duration(minutes)*time.Minute {
		return "stale"
	}
	return "normal"
}
func RetentionCutoff(now time.Time, days int) time.Time { return now.AddDate(0, 0, -days) }
func DecodeJSON(data []byte) (Batch, error) {
	var b Batch
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e := d.Decode(&b); e != nil {
		return b, &Error{"INVALID_JSON", "unknown field or malformed JSON"}
	}
	if e := d.Decode(&struct{}{}); !errors.Is(e, io.EOF) {
		return b, &Error{"INVALID_JSON", "request must contain exactly one JSON document"}
	}
	return b, nil
}
func (s *Service) Ready() error {
	f, err := os.CreateTemp(s.AssetDir, ".readiness-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err = f.Write([]byte("ok")); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	removeErr := os.Remove(name)
	if err != nil {
		return err
	}
	return removeErr
}
