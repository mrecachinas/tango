package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	MongoURL      string  `json:"mongo_url" yaml:"mongo_url"`
	MongoPort     int     `json:"mongo_port" yaml:"mongo_port"`
	MongoUser     string  `json:"mongo_user" yaml:"mongo_user"`
	MongoPassword string  `json:"mongo_password" yaml:"mongo_password"`
	MongoDB       string  `json:"mongo_db" yaml:"mongo_db"`
	RefreshRate   float64 `json:"refresh_rate" yaml:"refresh_rate"`
}

type ProfileConfig struct {
	Profiles map[string]Config `yaml:"profiles"`
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
	return profile, nil
}
