package config

import (
	"bytes"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	MinimaxSecret string `toml:"minimax_secret"`
	MinimaxGroup  string `toml:"minimax_group_id"`
}

func Load(path string) (Config, error) {
	cfg := Config{}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func (c Config) IsComplete() bool {
	return c.MinimaxSecret != "" && c.MinimaxGroup != ""
}
