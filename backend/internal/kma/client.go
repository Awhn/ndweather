package kma

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ndweather/backend/internal/ingest"
	"ndweather/backend/internal/store"
)

type Client struct {
	Endpoint, ServiceKey string
	HTTP                 *http.Client
	Location             *time.Location
}

type item struct {
	BaseDate, BaseTime, Category, FcstDate, FcstTime, FcstValue string
}

type response struct {
	Response struct {
		Header struct {
			ResultCode, ResultMsg string
		} `json:"header"`
		Body struct {
			Items struct {
				Item []item `json:"item"`
			} `json:"items"`
		} `json:"body"`
	} `json:"response"`
}

func (c *Client) Fetch(ctx context.Context, site store.Site, now time.Time) (ingest.Batch, error) {
	loc := c.Location
	if loc == nil {
		loc = time.Local
	}
	base := latestBase(now.In(loc))
	u, err := url.Parse(strings.TrimRight(c.Endpoint, "/") + "/getVilageFcst")
	if err != nil {
		return ingest.Batch{}, err
	}
	q := u.Query()
	serviceKey := c.ServiceKey
	if decoded, decodeErr := url.QueryUnescape(serviceKey); decodeErr == nil {
		serviceKey = decoded
	}
	q.Set("serviceKey", serviceKey)
	q.Set("pageNo", "1")
	q.Set("numOfRows", "1000")
	q.Set("dataType", "JSON")
	q.Set("base_date", base.Format("20060102"))
	q.Set("base_time", base.Format("1504"))
	q.Set("nx", strconv.Itoa(site.GridX))
	q.Set("ny", strconv.Itoa(site.GridY))
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ingest.Batch{}, err
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 20 * time.Second}
	}
	res, err := h.Do(req)
	if err != nil {
		return ingest.Batch{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ingest.Batch{}, fmt.Errorf("KMA HTTP status %d", res.StatusCode)
	}
	var payload response
	if err = json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return ingest.Batch{}, fmt.Errorf("decode KMA response: %w", err)
	}
	if payload.Response.Header.ResultCode != "00" {
		return ingest.Batch{}, fmt.Errorf("KMA %s: %s", payload.Response.Header.ResultCode, payload.Response.Header.ResultMsg)
	}
	return normalize(site, base, now.In(loc), loc, payload.Response.Body.Items.Item)
}

func latestBase(now time.Time) time.Time {
	times := []int{2, 5, 8, 11, 14, 17, 20, 23}
	available := now.Add(-10 * time.Minute)
	for i := len(times) - 1; i >= 0; i-- {
		candidate := time.Date(available.Year(), available.Month(), available.Day(), times[i], 0, 0, 0, available.Location())
		if !candidate.After(available) {
			return candidate
		}
	}
	yesterday := available.AddDate(0, 0, -1)
	return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 0, 0, 0, available.Location())
}

func normalize(site store.Site, issued, now time.Time, loc *time.Location, items []item) (ingest.Batch, error) {
	values := map[time.Time]map[string]string{}
	for _, x := range items {
		t, err := time.ParseInLocation("200601021504", x.FcstDate+x.FcstTime, loc)
		if err != nil {
			continue
		}
		if values[t] == nil {
			values[t] = map[string]string{}
		}
		values[t][x.Category] = x.FcstValue
	}
	if len(values) == 0 {
		return ingest.Batch{}, fmt.Errorf("KMA response contains no forecast items")
	}
	dailyMin, dailyMax := map[string]*float64{}, map[string]*float64{}
	for t, v := range values {
		day := t.Format("20060102")
		if n, ok := number(v["TMN"]); ok {
			dailyMin[day] = &n
		}
		if n, ok := number(v["TMX"]); ok {
			dailyMax[day] = &n
		}
	}
	b := ingest.Batch{SchemaVersion: "1.0", BatchID: "KMA-" + site.Code + "-" + issued.Format("20060102-1504"), Source: "KMA-VilageFcstInfoService-2.0", CreatedAt: now.Format(time.RFC3339)}
	var nearest time.Time
	for t, v := range values {
		if t.Before(now.Add(-time.Hour)) {
			continue
		}
		pop, _ := integer(v["POP"])
		b.Records.Forecasts = append(b.Records.Forecasts, ingest.Forecast{SiteCode: site.Code, IssuedAt: issued.Format(time.RFC3339), ValidAt: t.Format(time.RFC3339), MinTemperature: dailyMin[t.Format("20060102")], MaxTemperature: dailyMax[t.Format("20060102")], RainProbability: pop, Sky: sky(v["SKY"])})
		if nearest.IsZero() || t.Before(nearest) {
			nearest = t
		}
	}
	if nearest.IsZero() {
		return ingest.Batch{}, fmt.Errorf("KMA response contains no current forecast")
	}
	v := values[nearest]
	temp, _ := pointer(v["TMP"])
	humidity, _ := pointer(v["REH"])
	wind, _ := pointer(v["WSD"])
	precip := precipitation(v["PCP"])
	b.Records.Observations = []ingest.Observation{{SiteCode: site.Code, ObservedAt: nearest.Format(time.RFC3339), Temperature: temp, Humidity: humidity, WindDirection: direction(v["VEC"]), WindSpeed: wind, Precipitation: &precip, PrecipitationState: precipitationState(v["PTY"]), Sky: sky(v["SKY"])}}
	return b, nil
}

func number(s string) (float64, bool) { n, e := strconv.ParseFloat(s, 64); return n, e == nil }
func pointer(s string) (*float64, bool) {
	n, ok := number(s)
	if !ok {
		return nil, false
	}
	return &n, true
}
func integer(s string) (*int, bool) {
	n, e := strconv.Atoi(s)
	if e != nil {
		return nil, false
	}
	return &n, true
}
func sky(v string) string {
	if v == "1" {
		return "맑음"
	}
	if v == "3" {
		return "구름많음"
	}
	if v == "4" {
		return "흐림"
	}
	return ""
}
func precipitationState(v string) string {
	return map[string]string{"0": "없음", "1": "비", "2": "비/눈", "3": "눈", "4": "소나기"}[v]
}
func precipitation(v string) float64 {
	fields := strings.Fields(v)
	if len(fields) == 0 || v == "강수없음" {
		return 0
	}
	n, _ := strconv.ParseFloat(fields[0], 64)
	return n
}
func direction(v string) string {
	n, ok := number(v)
	if !ok {
		return ""
	}
	names := []string{"북", "북동", "동", "남동", "남", "남서", "서", "북서"}
	return names[int(math.Floor((n+22.5)/45))%8]
}
