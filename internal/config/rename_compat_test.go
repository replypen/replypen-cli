package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvBaseURLAcceptsLegacyAndRefusesConflict(t *testing.T) {
	clearEnv(t)
	t.Setenv(legacyEnvBaseURL, "https://staging.example")
	res, err := load("", t.TempDir())
	if err != nil || res.BaseURL != "https://staging.example" || res.BaseURLSource != legacyEnvBaseURL {
		t.Fatalf("legacy env: %+v, %v", res, err)
	}

	t.Setenv(envBaseURL, "https://staging.example") // same value: fine, new name reported
	if res, err = load("", t.TempDir()); err != nil || res.BaseURLSource != envBaseURL {
		t.Fatalf("both equal: %+v, %v", res, err)
	}

	t.Setenv(envBaseURL, "https://other.example")
	if _, err = load("", t.TempDir()); err == nil || !strings.Contains(err.Error(), legacyEnvBaseURL) {
		t.Fatalf("conflicting env must error naming both, got %v", err)
	}
}

func writeMarker(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverBrainLegacyMarkerWalkUp(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, LegacyMarkerFileName, "project = \"dentai\"\n")
	writeMarker(t, root, legacyLocalFileName, "tenant = \"de-kies\"\n")
	sub := filepath.Join(root, "skills", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	clearEnv(t)
	res, err := load("", sub)
	if err != nil {
		t.Fatal(err)
	}
	if res.Project != "dentai" || res.MarkerName() != LegacyMarkerFileName {
		t.Fatalf("legacy marker: project %q marker %q", res.Project, res.MarkerName())
	}
	if res.Tenant != "de-kies" || res.TenantSource != legacyLocalFileName {
		t.Fatalf("legacy local overlay: tenant %q source %q", res.Tenant, res.TenantSource)
	}

	// The new overlay, once present, wins even when it sets no tenant.
	writeMarker(t, root, LocalFileName, "# nothing\n")
	if res, err = load("", sub); err != nil || res.Tenant != "" {
		t.Fatalf("new overlay must shadow the legacy one: tenant %q, %v", res.Tenant, err)
	}
}

func TestDiscoverBrainBothMarkers(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, MarkerFileName, "project = \"dentai\"\n")
	writeMarker(t, dir, LegacyMarkerFileName, "project = \"dentai\"\n")
	b, err := DiscoverBrain(dir)
	if err != nil || b.MarkerFile != MarkerFileName {
		t.Fatalf("identical pair: %+v, %v", b, err)
	}

	// A rename keeps an identical legacy twin identical.
	if _, err := UpdateBrainProject(b, "dentai", "dentai-be"); err != nil {
		t.Fatal(err)
	}
	if b, err = DiscoverBrain(dir); err != nil || b.Project != "dentai-be" {
		t.Fatalf("after rename: %+v, %v", b, err)
	}

	writeMarker(t, dir, LegacyMarkerFileName, "project = \"other\"\n")
	if _, err := DiscoverBrain(dir); err == nil || !strings.Contains(err.Error(), LegacyMarkerFileName) || !strings.Contains(err.Error(), MarkerFileName) {
		t.Fatalf("differing pair must error naming both, got %v", err)
	}
}
