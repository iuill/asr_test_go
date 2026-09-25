package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
