package minimax

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

var (
	rng   = randSource()
	rngMu sync.Mutex
)

func randSource() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

type Client struct {
	apiKey     string
	groupID    string
	httpClient *http.Client
}

type VoiceCloneResponse struct {
	InputSensitive     bool   `json:"input_sensitive"`
	InputSensitiveType int    `json:"input_sensitive_type"`
	DemoAudio          string `json:"demo_audio"`
	BaseResp           struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

type UploadResponse struct {
	File struct {
		FileID    int64  `json:"file_id"`
		Bytes     int64  `json:"bytes"`
		CreatedAt int64  `json:"created_at"`
		Filename  string `json:"filename"`
		Purpose   string `json:"purpose"`
	} `json:"file"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

type CloneResult struct {
	FileID    string
	VoiceID   string
	StatusMsg string
}

func NewClient(apiKey, groupID string) *Client {
	return &Client{
		apiKey:     apiKey,
		groupID:    groupID,
		httpClient: &http.Client{Timeout: 45 * time.Second},
	}
}

func (c *Client) CloneVoice(ctx context.Context, filePath string) (*CloneResult, error) {
	if c.apiKey == "" || c.groupID == "" {
		return nil, fmt.Errorf("missing MiniMax credentials")
	}

	voiceID, err := GenerateVoiceID(filePath)
	if err != nil {
		return nil, fmt.Errorf("generate voice id: %w", err)
	}

	uploadResp, err := c.UploadFile(ctx, filePath)
	if err != nil {
		return nil, fmt.Errorf("upload file: %w", err)
	}

	cloneResp, err := c.CloneWithFileID(ctx, uploadResp.File.FileID, voiceID)
	if err != nil {
		return nil, fmt.Errorf("clone voice: %w", err)
	}

	return &CloneResult{
		FileID:    strconv.FormatInt(uploadResp.File.FileID, 10),
		VoiceID:   voiceID,
		StatusMsg: cloneResp.BaseResp.StatusMsg,
	}, nil
}

func (c *Client) UploadFile(ctx context.Context, filePath string) (*UploadResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("missing MiniMax API key")
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", err)
	}

	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("purpose", "voice_clone"); err != nil {
		return nil, fmt.Errorf("write multipart field: %w", err)
	}

	part, err := writer.CreateFormFile("file", filepath.Base(absPath))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.minimaxi.com/v1/files/upload", &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute upload request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upload response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result UploadResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode upload response: %w", err)
	}

	if result.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax upload failed: %d %s", result.BaseResp.StatusCode, result.BaseResp.StatusMsg)
	}

	return &result, nil
}

func (c *Client) CloneWithFileID(ctx context.Context, fileID int64, voiceID string) (*VoiceCloneResponse, error) {
	if c.apiKey == "" || c.groupID == "" {
		return nil, fmt.Errorf("missing MiniMax credentials")
	}
	url := fmt.Sprintf("https://api.minimaxi.com/v1/voice_clone?GroupId=%s", c.groupID)

	payload := map[string]any{
		"file_id":  fileID,
		"voice_id": voiceID,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute clone request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read clone response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clone failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result VoiceCloneResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode clone response: %w", err)
	}

	if result.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax clone failed: %d %s", result.BaseResp.StatusCode, result.BaseResp.StatusMsg)
	}

	return &result, nil
}

func GenerateVoiceID(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path for hash: %w", err)
	}

	file, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("open file for hash: %w", err)
	}
	defer file.Close()

	hasher := md5.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}

	sum := hasher.Sum(nil)
	full := hex.EncodeToString(sum)
	return fmt.Sprintf("minimax-voice-%s", full[len(full)-6:]), nil
}
