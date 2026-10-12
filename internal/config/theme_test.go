package config

import (
	"os"
	"path/filepath"
	"testing"

	"tango/internal/theme"
)

func TestThemeGlobalAndProfileOverride(t *testing.T) {
	path := writeTempYAML(t, `
theme:
  name: auto
  colors:
    accent: "#89b4fa"
    border: "238"
profiles:
  inherited:
    uri: mongodb://localhost
  override:
    theme:
      name: light
      colors:
        accent: "34"
`)
	t.Setenv("HOME", filepath.Dir(path))
	global, err := LoadTheme()
	if err != nil || global.Name != "auto" || global.Colors["border"] != "238" {
		t.Fatalf("global: %+v %v", global, err)
	}
	inherited, err := loadProfileFromPath(path, "inherited")
	if err != nil || inherited.Theme.Name != "auto" || inherited.Theme.Colors["accent"] != "#89b4fa" {
		t.Fatalf("inherited: %+v %v", inherited.Theme, err)
	}
	override, err := loadProfileFromPath(path, "override")
	if err != nil || override.Theme.Name != "light" || override.Theme.Colors["accent"] != "34" || override.Theme.Colors["border"] != "238" {
		t.Fatalf("override: %+v %v", override.Theme, err)
	}
	if global.Colors["accent"] != "#89b4fa" {
		t.Fatal("profile changed global palette")
	}
}

func TestLoadThemeMissingInvalidAndNoColor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	skin, err := LoadTheme()
	if err != nil || skin.Name != "" {
		t.Fatalf("missing config: %+v %v", skin, err)
	}
	path := filepath.Join(os.Getenv("HOME"), ".tango.yaml")
	if err := os.WriteFile(path, []byte("theme: [invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTheme(); err == nil {
		t.Fatal("invalid YAML was accepted")
	}
	t.Setenv("NO_COLOR", "1")
	active, err := theme.Load(theme.Skin{Name: "light", Colors: map[string]string{"accent": "34"}}, true)
	if err != nil || !active.Mono {
		t.Fatalf("NO_COLOR: %+v %v", active, err)
	}
}
