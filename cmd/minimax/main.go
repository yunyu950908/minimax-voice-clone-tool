package main

import (
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"

	"minimax/internal/app"
	"minimax/internal/config"
	"minimax/internal/logging"
	"minimax/internal/system"
)

func main() {
	zerolog.TimeFieldFormat = time.RFC3339

	paths, err := system.ResolvePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法解析路径: %v\n", err)
		os.Exit(1)
	}

	if err := system.EnsureDirs(paths); err != nil {
		fmt.Fprintf(os.Stderr, "无法创建目录: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取配置失败: %v\n", err)
		os.Exit(1)
	}

	logger, cleanupLogger, err := logging.Setup(paths.LogFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer cleanupLogger()

	startDir, err := os.Getwd()
	if err != nil {
		logger.Warn().Err(err).Msg("无法获取当前工作目录，使用默认路径 .")
		startDir = "."
	}

	tui := app.New(cfg, paths, logger, startDir)

	if err := tui.Run(); err != nil {
		logger.Error().Err(err).Msg("application exited with error")
		fmt.Fprintf(os.Stderr, "程序异常退出: %v\n", err)
		os.Exit(1)
	}
}
