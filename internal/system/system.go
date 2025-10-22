package system

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	ConfigDir    string
	ConfigFile   string
	DataDir      string
	LogsDir      string
	LogFile      string
	DBFile       string
	DownloadsDir string
}

func ResolvePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home dir: %w", err)
	}

	configDir := filepath.Join(home, ".minimax")
	dataDir := filepath.Join(home, "minimax")
	logsDir := filepath.Join(dataDir, "logs")
	downloadsDir := filepath.Join(home, "Downloads")

	return Paths{
		ConfigDir:    configDir,
		ConfigFile:   filepath.Join(configDir, "config.toml"),
		DataDir:      dataDir,
		LogsDir:      logsDir,
		LogFile:      filepath.Join(logsDir, "app.log"),
		DBFile:       filepath.Join(dataDir, "minimax.db"),
		DownloadsDir: downloadsDir,
	}, nil
}

func EnsureDirs(paths Paths) error {
	dirs := []string{
		paths.ConfigDir,
		paths.DataDir,
		paths.LogsDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	return nil
}
