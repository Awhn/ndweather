package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const testSites = "- code: ONE\n  name: One\n  latitude: 1\n  longitude: 1\n  forecastGridX: 1\n  forecastGridY: 1\n  warningAreaCodes: []\n  displayOrder: 1\n  enabled: true\n"

func TestOpenAppliesConnectionPragmasAndMainReadinessWrite(t *testing.T) {
	dir := t.TempDir()
	sites := filepath.Join(dir, "sites.yaml")
	if err := os.WriteFile(sites, []byte(testSites), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(filepath.Join(dir, "weather.db"), sites)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	var foreignKeys, timeout int
	if err = s.DB.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 || timeout != 5000 {
		t.Fatalf("pragmas foreign_keys=%d busy_timeout=%d", foreignKeys, timeout)
	}
	if err = s.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRemovedSiteIsDisabledOnReopen(t *testing.T) {
	dir := t.TempDir()
	sites := filepath.Join(dir, "sites.yaml")
	dbPath := filepath.Join(dir, "weather.db")
	twoSites := testSites + "- code: TWO\n  name: Two\n  latitude: 2\n  longitude: 2\n  forecastGridX: 2\n  forecastGridY: 2\n  warningAreaCodes: []\n  displayOrder: 2\n  enabled: true\n"
	if err := os.WriteFile(sites, []byte(twoSites), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dbPath, sites)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.DB.Close()
	if err = os.WriteFile(sites, []byte(testSites), 0600); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbPath, sites)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	got, err := s.Sites()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Code != "ONE" {
		t.Fatalf("enabled sites = %+v", got)
	}
}

func TestUnknownSiteFieldIsRejected(t *testing.T) {
	dir := t.TempDir()
	sites := filepath.Join(dir, "sites.yaml")
	if err := os.WriteFile(sites, []byte(testSites+"  typoField: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(dir, "weather.db"), sites); err == nil {
		t.Fatal("unknown YAML field was accepted")
	}
}
