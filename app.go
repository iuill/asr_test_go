package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/oauth2/google"
)

type Config struct {
	Language string `json:"language"`
	OpenAI   struct {
		APIKey  string `json:"api_key"`
		BaseURL string `json:"base_url"`
	} `json:"openai"`
	AzureSpeech struct {
		APIKey   string `json:"api_key"`
		Endpoint string `json:"endpoint"`
	} `json:"azure_speech"`
	Google struct {
		ServiceAccount json.RawMessage `json:"service_account"`
		ProjectID      string          `json:"project_id"`
		Location       string          `json:"location"`
	} `json:"google"`
}

var localePattern = regexp.MustCompile(`(?i)^[a-z]{2}-[a-z]{2}$`)

func normalizeLanguage(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !localePattern.MatchString(value) {
		return "", errors.New("language は ja-JP のような言語-地域コードで指定してください")
	}
	return strings.ToLower(value[:2]) + "-" + strings.ToUpper(value[3:]), nil
}

func openAILanguage(cfg Config) string {
	return strings.ToLower(cfg.Language[:2])
}

type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}
type AppInfo struct {
	Version        string  `json:"version"`
	ConfigPath     string  `json:"configPath"`
	Models         []Model `json:"models"`
	DebugLogging   bool    `json:"debugLogging"`
	ShowTimestamps bool    `json:"showTimestamps"`
	AutoSave       bool    `json:"autoSave"`
	AutoSaveDir    string  `json:"autoSaveDir"`
	LogPath        string  `json:"logPath"`
	LogError       string  `json:"logError,omitempty"`
	Error          string  `json:"error,omitempty"`
}

var appVersion = "0.0.1"

type Transcript struct {
	Text string `json:"text"`
}
type App struct {
	ctx      context.Context
	client   *http.Client
	debug    debugLogger
	prefMu   sync.Mutex
	liveMu   sync.Mutex
	live     *liveSession
	autoMu   sync.Mutex
	autoPath string
}

func NewApp() *App { return &App{client: &http.Client{Timeout: 90 * time.Second}} }
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if pref, path, err := loadPreferences(); err == nil {
		a.debug.configure(pref.Enabled, path)
		a.debug.logf("INFO", "app started version=%s", appVersion)
	}
}

func (a *App) SaveTranscript(content string) (bool, error) {
	if len(content) > 10*1024*1024 {
		return false, errors.New("保存するテキストが大きすぎます")
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "文字起こし結果を保存",
		DefaultFilename: "transcript.txt",
		Filters:         []runtime.FileFilter{{DisplayName: "テキストファイル (*.txt)", Pattern: "*.txt"}},
	})
	if err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return false, err
	}
	return true, nil
}

func configPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "appsettings.json"), nil
}
func loadConfig() (Config, string, error) {
	var cfg Config
	path, err := configPath()
	if err != nil {
		return cfg, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, path, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return cfg, path, fmt.Errorf("設定ファイルのJSONが不正です: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return cfg, path, errors.New("設定ファイルに余分なJSONデータがあります")
	}
	cfg.Language, err = normalizeLanguage(cfg.Language)
	if err != nil {
		return cfg, path, err
	}
	return cfg, path, nil
}
func (a *App) GetInfo() AppInfo {
	cfg, path, err := loadConfig()
	info := AppInfo{Version: appVersion, ConfigPath: path}
	if path != "" {
		info.AutoSaveDir = filepath.Join(filepath.Dir(path), "transcripts")
	}
	a.prefMu.Lock()
	pref, prefPath, prefErr := loadPreferences()
	if prefErr == nil {
		a.debug.configure(pref.Enabled, prefPath)
		info.ShowTimestamps = pref.ShowTimestamps
		info.AutoSave = pref.AutoSave
	} else {
		a.debug.configure(false, prefPath)
		info.LogError = prefErr.Error()
	}
	a.debug.logf("INFO", "settings reloaded config_valid=%t", err == nil)
	info.DebugLogging, info.LogPath, _ = a.debug.status()
	if _, _, logErr := a.debug.status(); logErr != nil {
		info.LogError = logErr.Error()
	}
	a.prefMu.Unlock()
	if err != nil {
		info.Error = err.Error()
	}
	info.Models = configuredModels(cfg)
	if err != nil {
		for i := range info.Models {
			info.Models[i].Available = false
			info.Models[i].Reason = "設定ファイルを修正してください"
		}
	}
	return info
}

func (a *App) LogDiagnostic(code string) {
	switch code {
	case "microphone_started", "microphone_stopped", "microphone_error", "audio_processing_error", "live_ui_error":
		a.debug.logf("TRACE", "ui event=%s", code)
	}
}

func configuredModels(cfg Config) []Model {
	openAIReason := ""
	if strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		openAIReason = "openai.api_key が空です"
	} else if cfg.OpenAI.BaseURL != "" && !validHTTPSURL(cfg.OpenAI.BaseURL, false) {
		openAIReason = "openai.base_url がHTTPS URLではありません"
	}
	azureReason := ""
	if strings.TrimSpace(cfg.AzureSpeech.APIKey) == "" {
		azureReason = "azure_speech.api_key が空です"
	} else if !validHTTPSURL(cfg.AzureSpeech.Endpoint, true) || strings.Contains(cfg.AzureSpeech.Endpoint, "YOUR-RESOURCE") {
		azureReason = "azure_speech.endpoint に実際のHTTPS URLを設定してください"
	}
	googleReason := ""
	if strings.TrimSpace(cfg.Google.ProjectID) == "" {
		googleReason = "google.project_id が空です"
	} else if len(cfg.Google.ServiceAccount) == 0 || string(cfg.Google.ServiceAccount) == "null" {
		googleReason = "google.service_account が空です"
	} else if _, err := google.JWTConfigFromJSON(cfg.Google.ServiceAccount, "https://www.googleapis.com/auth/cloud-platform"); err != nil {
		googleReason = "google.service_account が不正です"
	}
	chirpReason := googleReason
	if chirpReason == "" && cfg.Google.Location != "" && cfg.Google.Location != "us" && cfg.Google.Location != "eu" {
		chirpReason = "google.location は us または eu にしてください"
	}
	return []Model{
		{"gpt-transcribe", "GPT Transcribe", "OpenAI", openAIReason == "", openAIReason},
		{"gpt-live-transcribe", "GPT Live Transcribe", "OpenAI", openAIReason == "", openAIReason},
		{"azure-speech", "Azure AI Speech", "Azure", azureReason == "", azureReason},
		{"azure-speech-diarize", "Azure AI Speech · 話者識別", "Azure", azureReason == "", azureReason},
		{"google-v1", "Google Speech-to-Text V1", "Google", googleReason == "", googleReason},
		{"google-chirp-3", "Google Chirp 3", "Google", chirpReason == "", chirpReason},
	}
}

func validHTTPSURL(raw string, rootOnly bool) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (!rootOnly || u.Path == "" || u.Path == "/")
}

// Transcribe accepts one bounded WAV utterance. Keys remain in Go, outside WebView JavaScript.
func (a *App) Transcribe(modelID, audioBase64 string) (result Transcript, err error) {
	started := time.Now()
	a.debug.logf("TRACE", "transcription started model=%s", safeModelID(modelID))
	defer func() {
		if err != nil {
			a.debug.logf("ERROR", "transcription failed model=%s elapsed_ms=%d", safeModelID(modelID), time.Since(started).Milliseconds())
		} else {
			a.debug.logf("INFO", "transcription completed model=%s elapsed_ms=%d", safeModelID(modelID), time.Since(started).Milliseconds())
		}
	}()
	cfg, _, err := loadConfig()
	if err != nil {
		return Transcript{}, err
	}
	audio, err := base64.StdEncoding.DecodeString(audioBase64)
	if err != nil {
		return Transcript{}, errors.New("音声データを読み取れません")
	}
	if len(audio) < 44 || len(audio) > 25*1024*1024 || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		return Transcript{}, errors.New("WAV音声が不正か、25MBを超えています")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 90*time.Second)
	defer cancel()
	switch modelID {
	case "gpt-transcribe":
		return a.openAI(ctx, cfg, modelID, audio)
	case "azure-speech", "azure-speech-diarize":
		return a.azure(ctx, cfg, modelID == "azure-speech-diarize", audio)
	case "google-v1", "google-chirp-3":
		return a.google(ctx, cfg, modelID == "google-chirp-3", audio)
	default:
		return Transcript{}, errors.New("未対応のモデルです")
	}
}
func (a *App) openAI(ctx context.Context, cfg Config, model string, audio []byte) (Transcript, error) {
	if cfg.OpenAI.APIKey == "" {
		return Transcript{}, errors.New("OpenAI APIキーが未設定です")
	}
	base := strings.TrimRight(cfg.OpenAI.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if !validHTTPSURL(base, false) {
		return Transcript{}, errors.New("OpenAI base_url は HTTPS URL にしてください")
	}
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	if err := w.WriteField("model", model); err != nil {
		return Transcript{}, err
	}
	if err := w.WriteField("languages[]", openAILanguage(cfg)); err != nil {
		return Transcript{}, err
	}
	part, err := w.CreateFormFile("file", "speech.wav")
	if err != nil {
		return Transcript{}, err
	}
	if _, err = part.Write(audio); err != nil {
		return Transcript{}, err
	}
	if err = w.Close(); err != nil {
		return Transcript{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/audio/transcriptions", buf)
	if err != nil {
		return Transcript{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAI.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	data, err := a.do(req)
	if err != nil {
		return Transcript{}, err
	}
	var result Transcript
	if err := json.Unmarshal(data, &result); err != nil {
		return Transcript{}, fmt.Errorf("OpenAIの応答を解析できません: %w", err)
	}
	return result, nil
}
func (a *App) azure(ctx context.Context, cfg Config, diarize bool, audio []byte) (Transcript, error) {
	az := cfg.AzureSpeech
	if az.APIKey == "" || az.Endpoint == "" {
		return Transcript{}, errors.New("Azure Speech のキーとエンドポイントが未設定です")
	}
	if !validHTTPSURL(az.Endpoint, true) {
		return Transcript{}, errors.New("Azure endpoint は HTTPS のルートURLにしてください")
	}
	definition := map[string]any{"locales": []string{cfg.Language}}
	if diarize {
		definition["diarization"] = map[string]any{"enabled": true, "maxSpeakers": 2}
	}
	def, _ := json.Marshal(definition)
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	if err := w.WriteField("definition", string(def)); err != nil {
		return Transcript{}, err
	}
	part, err := w.CreateFormFile("audio", "speech.wav")
	if err != nil {
		return Transcript{}, err
	}
	if _, err = part.Write(audio); err != nil {
		return Transcript{}, err
	}
	if err = w.Close(); err != nil {
		return Transcript{}, err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(az.Endpoint), "/") + "/speechtotext/transcriptions:transcribe?api-version=2025-10-15"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, buf)
	if err != nil {
		return Transcript{}, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", az.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	data, err := a.do(req)
	if err != nil {
		return Transcript{}, err
	}
	var result struct {
		CombinedPhrases []struct {
			Text string `json:"text"`
		} `json:"combinedPhrases"`
		Phrases []struct {
			Text    string `json:"text"`
			Speaker *int   `json:"speaker"`
		} `json:"phrases"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Transcript{}, fmt.Errorf("Azureの応答を解析できません: %w", err)
	}
	if diarize && len(result.Phrases) > 0 {
		lines := make([]string, 0, len(result.Phrases))
		for _, p := range result.Phrases {
			if p.Speaker != nil {
				lines = append(lines, fmt.Sprintf("話者 %d: %s", *p.Speaker+1, p.Text))
			} else {
				lines = append(lines, p.Text)
			}
		}
		return Transcript{Text: strings.Join(lines, "\n")}, nil
	}
	if len(result.CombinedPhrases) > 0 {
		return Transcript{Text: result.CombinedPhrases[0].Text}, nil
	}
	return Transcript{}, nil
}
func (a *App) do(req *http.Request) ([]byte, error) {
	started := time.Now()
	resp, err := a.client.Do(req)
	if err != nil {
		a.debug.logf("ERROR", "api transport failed elapsed_ms=%d error_type=%T", time.Since(started).Milliseconds(), err)
		return nil, err
	}
	defer resp.Body.Close()
	a.debug.logf("TRACE", "api response status=%d elapsed_ms=%d", resp.StatusCode, time.Since(started).Milliseconds())
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("APIエラー (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (a *App) google(ctx context.Context, cfg Config, chirp3 bool, audio []byte) (Transcript, error) {
	g := cfg.Google
	if len(g.ServiceAccount) < 3 || g.ProjectID == "" {
		return Transcript{}, errors.New("Googleサービスアカウントとproject_idが未設定です")
	}
	jwt, err := google.JWTConfigFromJSON(g.ServiceAccount, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return Transcript{}, fmt.Errorf("Googleサービスアカウントが不正です: %w", err)
	}
	token, err := jwt.TokenSource(ctx).Token()
	if err != nil {
		return Transcript{}, fmt.Errorf("Google認証に失敗しました: %w", err)
	}
	var endpoint string
	var payload any
	if chirp3 {
		location := g.Location
		if location == "" {
			location = "us"
		}
		if location != "us" && location != "eu" {
			return Transcript{}, errors.New("Chirp 3 のlocationは us または eu にしてください")
		}
		endpoint = fmt.Sprintf("https://speech.googleapis.com/v2/projects/%s/locations/%s/recognizers/_:recognize", url.PathEscape(g.ProjectID), location)
		payload = map[string]any{"config": map[string]any{"autoDecodingConfig": map[string]any{}, "model": "chirp_3", "languageCodes": []string{cfg.Language}}, "content": base64.StdEncoding.EncodeToString(audio)}
	} else {
		endpoint = "https://speech.googleapis.com/v1/speech:recognize"
		payload = map[string]any{"config": map[string]any{"encoding": "LINEAR16", "sampleRateHertz": 16000, "languageCode": cfg.Language}, "audio": map[string]any{"content": base64.StdEncoding.EncodeToString(audio)}}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Transcript{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-user-project", g.ProjectID)
	data, err := a.do(req)
	if err != nil {
		return Transcript{}, err
	}
	var result struct {
		Results []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Transcript{}, fmt.Errorf("Googleの応答を解析できません: %w", err)
	}
	lines := make([]string, 0, len(result.Results))
	for _, r := range result.Results {
		if len(r.Alternatives) > 0 {
			lines = append(lines, r.Alternatives[0].Transcript)
		}
	}
	return Transcript{Text: strings.Join(lines, "\n")}, nil
}
