package exporter

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Record 表示一次克隆或上传尝试的结果，用于导出 CSV。
type Record struct {
	FilePath       string
	MinimaxFileID  string
	MinimaxVoiceID string
	Status         string
	ErrorReason    string
	UpdatedAt      time.Time
}

func ToCSV(records []Record, downloadsDir string) (string, error) {
	if downloadsDir == "" {
		return "", fmt.Errorf("downloads directory not provided")
	}

	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure downloads directory: %w", err)
	}

	if len(records) == 0 {
		return "", fmt.Errorf("没有可导出的记录")
	}

	filename := fmt.Sprintf("minimax_voice_export_%s.csv", time.Now().Format("20060102_150405"))
	fullPath := filepath.Join(downloadsDir, filename)

	file, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create export file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{"file_path", "minimax_file_id", "minimax_voice_id", "status", "error_reason", "updated_at"}
	if err := writer.Write(header); err != nil {
		return "", fmt.Errorf("write header: %w", err)
	}

	for _, rec := range records {
		row := []string{
			rec.FilePath,
			rec.MinimaxFileID,
			rec.MinimaxVoiceID,
			rec.Status,
			rec.ErrorReason,
		}
		if !rec.UpdatedAt.IsZero() {
			row = append(row, rec.UpdatedAt.Format(time.RFC3339))
		} else {
			row = append(row, "")
		}

		if err := writer.Write(row); err != nil {
			return "", fmt.Errorf("write row: %w", err)
		}
	}

	return fullPath, nil
}
