package apihub

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ndweather/backend/internal/ingest"
	"ndweather/backend/internal/store"
)

type Client struct {
	Endpoint, AuthKey string
	HTTP              *http.Client
	Location          *time.Location
}

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, string, error) {
	u, err := url.Parse(strings.TrimRight(c.Endpoint, "/") + path)
	if err != nil {
		return nil, "", err
	}
	query.Set("authKey", c.AuthKey)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := h.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("API Hub HTTP status %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 25<<20))
	return b, res.Header.Get("Content-Type"), err
}

func (c *Client) FetchRadar(ctx context.Context, now time.Time) (ingest.Batch, error) {
	q := url.Values{"cmp": {"HSR"}, "qcd": {"MSK"}, "obs": {"ECHO"}, "map": {"HB"}, "disp": {"A"}}
	b, contentType, err := c.get(ctx, "/api/typ01/cgi-bin/url/nph-rdr_cmp1_api", q)
	if err != nil {
		return ingest.Batch{}, err
	}
	if len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n" {
		contentType = "image/png"
	} else if len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff {
		contentType = "image/jpeg"
	} else {
		return ingest.Batch{}, fmt.Errorf("API Hub radar returned unsupported content type %q", contentType)
	}
	sum := sha256.Sum256(b)
	id := "APIHUB-RADAR-" + hex.EncodeToString(sum[:8])
	batch := newBatch("RADAR", now)
	batch.Assets = []ingest.Asset{{AssetID: id, AssetType: "radar", FileName: id, ContentType: contentType, ObservedAt: now.Format(time.RFC3339), Encoding: "base64", OriginalSize: len(b), SHA256: hex.EncodeToString(sum[:]), Payload: base64.StdEncoding.EncodeToString(b)}}
	return batch, nil
}

func (c *Client) FetchWarnings(ctx context.Context, sites []store.Site, now time.Time) (ingest.Batch, error) {
	loc := c.location()
	q := url.Values{"reg": {"0"}, "wrn": {"A"}, "tmfc1": {now.In(loc).Add(-24 * time.Hour).Format("200601021504")}, "tmfc2": {now.In(loc).Format("200601021504")}, "disp": {"0"}, "help": {"0"}}
	b, _, err := c.get(ctx, "/api/typ01/url/wrn_met_data.php", q)
	if err != nil {
		return ingest.Batch{}, err
	}
	rows := parseTable(string(b), "REG_ID", "WRN", "LVL", "CMD")
	latest := map[string]map[string]string{}
	for _, r := range rows {
		latest[r["REG_ID"]+"/"+r["WRN"]] = r
	}
	batch := newBatch("WARNING", now)
	for _, r := range latest {
		if r["CMD"] == "3" || r["CMD"] == "해제" {
			continue
		}
		announced, ok := parseTime(r["TM_FC"], loc)
		if !ok {
			continue
		}
		effective, ok := parseTime(r["TM_EF"], loc)
		if !ok {
			effective = announced
		}
		var siteCodes []string
		for _, site := range sites {
			for _, area := range site.Areas {
				if area == r["REG_ID"] {
					siteCodes = append(siteCodes, site.Code)
				}
			}
		}
		batch.Records.Warnings = append(batch.Records.Warnings, ingest.Warning{WarningID: "APIHUB-" + r["REG_ID"] + "-" + r["WRN"] + "-" + r["TM_FC"], Phenomenon: warningName(r["WRN"]), Level: warningLevel(r["LVL"]), AreaCode: r["REG_ID"], AreaName: r["REG_NAME"], AnnouncedAt: announced.Format(time.RFC3339), EffectiveAt: effective.Format(time.RFC3339), SiteCodes: siteCodes})
	}
	return batch, nil
}

func (c *Client) FetchTyphoons(ctx context.Context, now time.Time) (ingest.Batch, error) {
	q := url.Values{"tm": {now.UTC().Format("200601021504")}, "mode": {"2"}, "disp": {"0"}, "help": {"0"}}
	b, _, err := c.get(ctx, "/api/typ01/url/typ_now.php", q)
	if err != nil {
		return ingest.Batch{}, err
	}
	rows := parseTable(string(b), "TYP", "TYP_TM", "LAT", "LON")
	summaries := parseTable(string(b), "YY", "SEQ", "NOW", "TYP_NAME")
	names := map[string]string{}
	for _, r := range summaries {
		names[r["YY"]+"-"+r["SEQ"]] = r["TYP_NAME"]
	}
	batch := newBatch("TYPHOON", now)
	group := map[string][]map[string]string{}
	for _, r := range rows {
		group[r["YY"]+"-"+r["TYP"]] = append(group[r["YY"]+"-"+r["TYP"]], r)
	}
	for key, rs := range group {
		var analysis map[string]string
		for _, r := range rs {
			if r["FT"] == "0" {
				analysis = r
			}
		}
		if analysis == nil {
			continue
		}
		at, ok := parseUTC(analysis["TYP_TM"])
		if !ok {
			continue
		}
		pressure, _ := strconv.Atoi(analysis["PS"])
		wind, _ := strconv.ParseFloat(analysis["WS"], 64)
		speed, _ := strconv.ParseFloat(analysis["SP"], 64)
		lat, _ := strconv.ParseFloat(analysis["LAT"], 64)
		lon, _ := strconv.ParseFloat(analysis["LON"], 64)
		name := names[key]
		if name == "" {
			name = analysis["TYP_NAME"]
		}
		if name == "" {
			name = "제" + analysis["TYP"] + "호"
		}
		t := ingest.Typhoon{Key: "APIHUB-" + key, Number: analysis["TYP"], Name: name, Latitude: lat, Longitude: lon, Pressure: &pressure, MaxWind: &wind, Direction: analysis["DIR"], Speed: &speed, AnnouncedAt: at.Format(time.RFC3339), Active: true}
		for _, r := range rs {
			if r["FT"] != "1" {
				continue
			}
			ft, ok := parseUTC(r["FT_TM"])
			if !ok {
				continue
			}
			la, _ := strconv.ParseFloat(r["LAT"], 64)
			lo, _ := strconv.ParseFloat(r["LON"], 64)
			p, _ := strconv.Atoi(r["PS"])
			w, _ := strconv.ParseFloat(r["WS"], 64)
			t.ForecastPoints = append(t.ForecastPoints, ingest.Point{ForecastAt: ft.Format(time.RFC3339), Latitude: la, Longitude: lo, Pressure: &p, MaxWind: &w})
		}
		batch.Records.Typhoons = append(batch.Records.Typhoons, t)
	}
	return batch, nil
}

func newBatch(kind string, now time.Time) ingest.Batch {
	return ingest.Batch{SchemaVersion: "1.0", BatchID: "APIHUB-" + kind + "-" + now.UTC().Format("20060102-1504"), Source: "KMA-APIHub", CreatedAt: now.UTC().Format(time.RFC3339)}
}
func (c *Client) location() *time.Location {
	if c.Location != nil {
		return c.Location
	}
	return time.Local
}
func parseTime(v string, loc *time.Location) (time.Time, bool) {
	for _, f := range []string{"200601021504", "2006010215"} {
		if t, e := time.ParseInLocation(f, v, loc); e == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
func parseUTC(v string) (time.Time, bool) { t, ok := parseTime(v, time.UTC); return t, ok }

func parseTable(body string, required ...string) []map[string]string {
	var header []string
	var out []map[string]string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			candidate := strings.Fields(strings.TrimSpace(strings.TrimLeft(line, "#")))
			if containsAll(candidate, required) {
				header = candidate
			}
			continue
		}
		if line == "" || header == nil {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < len(header) {
			continue
		}
		row := map[string]string{}
		for i, k := range header {
			row[k] = fields[i]
		}
		out = append(out, row)
	}
	return out
}
func containsAll(fields, required []string) bool {
	set := map[string]bool{}
	for _, f := range fields {
		set[f] = true
	}
	for _, r := range required {
		if !set[r] {
			return false
		}
	}
	return true
}
func warningName(v string) string {
	if n := map[string]string{"W": "강풍", "R": "호우", "C": "한파", "D": "건조", "V": "풍랑", "T": "태풍", "S": "대설", "Y": "황사", "H": "폭염"}[v]; n != "" {
		return n
	}
	return v
}
func warningLevel(v string) string {
	if v == "1" || v == "W" {
		return "주의보"
	}
	if v == "2" || v == "A" {
		return "경보"
	}
	return v
}
