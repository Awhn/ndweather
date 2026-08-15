package config

import "testing"

func TestLoadValidation(t *testing.T) {
	t.Setenv("INGEST_MODE", "http")
	t.Setenv("INGEST_TOKEN", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected token validation")
	}

	t.Setenv("INGEST_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("PORT", "bad")
	if _, err := Load(); err == nil {
		t.Fatal("expected port validation")
	}
}

func TestLoadDefaultTimezone(t *testing.T) {
	t.Setenv("INGEST_MODE", "http")
	t.Setenv("INGEST_TOKEN", "0123456789abcdef0123456789abcdef")
	t.Setenv("TIMEZONE", "Asia/Seoul")

	config, err := Load()
	if err != nil {
		t.Fatalf("load configuration with embedded timezone data: %v", err)
	}
	if config.Timezone != "Asia/Seoul" {
		t.Fatalf("timezone = %q, want Asia/Seoul", config.Timezone)
	}
	if config.KMAEndpoint != "https://apis.data.go.kr/1360000/VilageFcstInfoService_2.0" || config.KMAPollSeconds != 600 {
		t.Fatalf("unexpected KMA defaults: endpoint=%q poll=%d", config.KMAEndpoint, config.KMAPollSeconds)
	}
	if config.KMAAPIHubEndpoint != "https://apihub.kma.go.kr" || config.KMAAPIHubPollSeconds != 600 {
		t.Fatalf("unexpected API Hub defaults: endpoint=%q poll=%d", config.KMAAPIHubEndpoint, config.KMAAPIHubPollSeconds)
	}
}

func TestLoadAcceptsForecastKeyAliasAndRejectsInvalidPort(t *testing.T) {
	t.Setenv("INGEST_MODE", "directory")
	t.Setenv("KMA_SERVICE_KEY", "")
	t.Setenv("KMA_API_KEY", " alias-key ")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.KMAServiceKey != "alias-key" {
		t.Fatalf("forecast key alias was not loaded: %q", config.KMAServiceKey)
	}
	t.Setenv("PORT", "65536")
	if _, err = Load(); err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	t.Setenv("INGEST_MODE", "directory")
	t.Setenv("LOG_LEVEL", "verbose")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid log level error")
	}
}
