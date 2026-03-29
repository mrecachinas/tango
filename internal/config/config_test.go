package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".tango.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing temp yaml: %v", err)
	}
	return path
}

func TestLoadProfile_FullConfig(t *testing.T) {
	yaml := `
profiles:
  prod:
    mongo_url: "mongo.example.com"
    mongo_port: 27017
    mongo_user: "admin"
    mongo_password: "secret"
    mongo_db: "mydb"
    refresh_rate: 5.5
`
	path := writeTempYAML(t, yaml)
	cfg, err := loadProfileFromPath(path, "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MongoURL != "mongo.example.com" {
		t.Errorf("MongoURL = %q, want %q", cfg.MongoURL, "mongo.example.com")
	}
	if cfg.MongoPort != 27017 {
		t.Errorf("MongoPort = %d, want %d", cfg.MongoPort, 27017)
	}
	if cfg.MongoUser != "admin" {
		t.Errorf("MongoUser = %q, want %q", cfg.MongoUser, "admin")
	}
	if cfg.MongoPassword != "secret" {
		t.Errorf("MongoPassword = %q, want %q", cfg.MongoPassword, "secret")
	}
	if cfg.MongoDB != "mydb" {
		t.Errorf("MongoDB = %q, want %q", cfg.MongoDB, "mydb")
	}
	if cfg.RefreshRate != 5.5 {
		t.Errorf("RefreshRate = %f, want %f", cfg.RefreshRate, 5.5)
	}
}

func TestLoadProfile_PartialConfig(t *testing.T) {
	yaml := `
profiles:
  dev:
    mongo_url: "localhost"
    mongo_port: 27017
`
	path := writeTempYAML(t, yaml)
	cfg, err := loadProfileFromPath(path, "dev")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MongoURL != "localhost" {
		t.Errorf("MongoURL = %q, want %q", cfg.MongoURL, "localhost")
	}
	if cfg.MongoPort != 27017 {
		t.Errorf("MongoPort = %d, want %d", cfg.MongoPort, 27017)
	}
	// Unset fields should be zero values
	if cfg.MongoUser != "" {
		t.Errorf("MongoUser = %q, want empty", cfg.MongoUser)
	}
	if cfg.MongoPassword != "" {
		t.Errorf("MongoPassword = %q, want empty", cfg.MongoPassword)
	}
	if cfg.MongoDB != "" {
		t.Errorf("MongoDB = %q, want empty", cfg.MongoDB)
	}
	if cfg.RefreshRate != 0 {
		t.Errorf("RefreshRate = %f, want 0", cfg.RefreshRate)
	}
}

func TestLoadProfile_MultipleProfiles(t *testing.T) {
	yaml := `
profiles:
  dev:
    mongo_url: "localhost"
    mongo_port: 27017
  prod:
    mongo_url: "prod.example.com"
    mongo_port: 27018
    mongo_db: "proddb"
`
	path := writeTempYAML(t, yaml)

	dev, err := loadProfileFromPath(path, "dev")
	if err != nil {
		t.Fatalf("unexpected error loading dev: %v", err)
	}
	if dev.MongoURL != "localhost" {
		t.Errorf("dev MongoURL = %q, want %q", dev.MongoURL, "localhost")
	}

	prod, err := loadProfileFromPath(path, "prod")
	if err != nil {
		t.Fatalf("unexpected error loading prod: %v", err)
	}
	if prod.MongoURL != "prod.example.com" {
		t.Errorf("prod MongoURL = %q, want %q", prod.MongoURL, "prod.example.com")
	}
	if prod.MongoDB != "proddb" {
		t.Errorf("prod MongoDB = %q, want %q", prod.MongoDB, "proddb")
	}
}

func TestLoadProfile_UnknownProfile(t *testing.T) {
	yaml := `
profiles:
  alpha:
    mongo_url: "a"
  beta:
    mongo_url: "b"
`
	path := writeTempYAML(t, yaml)
	_, err := loadProfileFromPath(path, "gamma")
	if err == nil {
		t.Fatal("expected error for unknown profile, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "gamma") {
		t.Errorf("error should mention requested profile name, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "alpha") || !strings.Contains(errMsg, "beta") {
		t.Errorf("error should list available profiles, got: %s", errMsg)
	}
}

func TestLoadProfile_InvalidYAML(t *testing.T) {
	path := writeTempYAML(t, `{{{not valid yaml:::`)
	_, err := loadProfileFromPath(path, "any")
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "could not parse") {
		t.Errorf("error should mention parsing failure, got: %s", err.Error())
	}
}

func TestLoadProfile_MissingFile(t *testing.T) {
	_, err := loadProfileFromPath("/nonexistent/path/.tango.yaml", "any")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "could not read") {
		t.Errorf("error should mention read failure, got: %s", err.Error())
	}
}
