package logging

import (
	"fmt"
	"os"

	"github.com/rs/zerolog"
)

func Setup(logPath string) (zerolog.Logger, func() error, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return zerolog.Logger{}, nil, fmt.Errorf("open log file: %w", err)
	}

	logger := zerolog.New(logFile).With().Timestamp().Logger()

	cleanup := func() error {
		return logFile.Close()
	}

	return logger, cleanup, nil
}
