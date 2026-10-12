package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"tango/internal/theme"
)

type Config struct {
	URI           string     `json:"uri" yaml:"uri"`
	MongoURL      string     `json:"mongo_url" yaml:"mongo_url"`
	MongoPort     int        `json:"mongo_port" yaml:"mongo_port"`
	MongoUser     string     `json:"mongo_user" yaml:"mongo_user"`
	MongoPassword string     `json:"mongo_password" yaml:"mongo_password"`
	MongoDB       string     `json:"mongo_db" yaml:"mongo_db"`
	AuthSource    string     `json:"auth_source" yaml:"auth_source"`
	RefreshRate   float64    `json:"refresh_rate" yaml:"refresh_rate"` // seconds between metrics polls
	Theme         theme.Skin `json:"theme" yaml:"theme"`
}

type ProfileConfig struct {
	Profiles map[string]Config `yaml:"profiles"`
	Theme    theme.Skin        `yaml:"theme"`
}

// LoadTheme loads the global theme even when no connection profile is used.
func LoadTheme() (theme.Skin, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return theme.Skin{}, err
	}
	data, err := os.ReadFile(filepath.Join(home, ".tango.yaml"))
	if os.IsNotExist(err) {
		return theme.Skin{}, nil
	}
	if err != nil {
		return theme.Skin{}, err
	}
	var pc ProfileConfig
	err = yaml.Unmarshal(data, &pc)
	return pc.Theme, err
}

func LoadProfile(profileName string) (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("could not determine home directory: %w", err)
	}
	configPath := filepath.Join(home, ".tango.yaml")
	return loadProfileFromPath(configPath, profileName)
}

func loadProfileFromPath(configPath, profileName string) (Config, error) {
	info, err := os.Stat(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("could not read %s: %w", configPath, err)
	}
	if info.Mode().Perm()&0077 != 0 {
		fmt.Fprintf(os.Stderr, "WARNING: %s has insecure permissions %o, should be 0600\n", configPath, info.Mode().Perm())
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Config{}, fmt.Errorf("could not read %s: %w", configPath, err)
	}
	var pc ProfileConfig
	if err := yaml.Unmarshal(data, &pc); err != nil {
		return Config{}, fmt.Errorf("could not parse %s: %w", configPath, err)
	}
	profile, ok := pc.Profiles[profileName]
	if !ok {
		names := make([]string, 0, len(pc.Profiles))
		for name := range pc.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		return Config{}, fmt.Errorf("profile %q not found; available profiles: %s", profileName, strings.Join(names, ", "))
	}
	if profile.Theme.Name == "" {
		profile.Theme.Name = pc.Theme.Name
	}
	colors := make(map[string]string, len(pc.Theme.Colors)+len(profile.Theme.Colors))
	for key, value := range pc.Theme.Colors {
		colors[key] = value
	}
	for key, value := range profile.Theme.Colors {
		colors[key] = value
	}
	profile.Theme.Colors = colors
	return profile, nil
}
