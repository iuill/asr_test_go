package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAIRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("model") != "gpt-transcribe" {
			t.Errorf("model = %q", r.FormValue("model"))
		}
		if r.FormValue("languages[]") != "en" {
			t.Errorf("language hint = %q", r.FormValue("languages[]"))
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if string(data) != "WAV" {
			t.Errorf("audio = %q", data)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"こんにちは"}`))
	}))
	defer server.Close()
	var cfg Config
	cfg.Language = "en-US"
	cfg.OpenAI.APIKey = "test-key"
	cfg.OpenAI.BaseURL = server.URL + "/v1"
	a := NewApp()
	a.client = server.Client()
	result, err := a.openAI(context.Background(), cfg, "gpt-transcribe", []byte("WAV"))
	if err != nil || result.Text != "こんにちは" {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

func TestAzureDiarizationRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speechtotext/transcriptions:transcribe" || r.URL.Query().Get("api-version") != "2025-10-15" {
			t.Errorf("unexpected URL: %s", r.URL)
		}
		if r.Header.Get("Ocp-Apim-Subscription-Key") != "azure-key" {
			t.Error("missing Azure key")
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		var def struct {
			Locales     []string `json:"locales"`
			Diarization struct {
				Enabled     bool `json:"enabled"`
				MaxSpeakers int  `json:"maxSpeakers"`
			} `json:"diarization"`
		}
		if err := json.Unmarshal([]byte(r.FormValue("definition")), &def); err != nil {
			t.Fatal(err)
		}
		if len(def.Locales) != 1 || def.Locales[0] != "fr-FR" || !def.Diarization.Enabled || def.Diarization.MaxSpeakers != 2 {
			t.Errorf("definition = %+v", def)
		}
		if _, _, err := r.FormFile("audio"); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"phrases":[{"text":"一つ目","speaker":0},{"text":"二つ目","speaker":1}]}`))
	}))
	defer server.Close()
	var cfg Config
	cfg.Language = "fr-FR"
	cfg.AzureSpeech.APIKey = "azure-key"
	cfg.AzureSpeech.Endpoint = server.URL
	a := NewApp()
	a.client = server.Client()
	result, err := a.azure(context.Background(), cfg, true, []byte("WAV"))
	if err != nil || !strings.Contains(result.Text, "話者 1: 一つ目") || !strings.Contains(result.Text, "話者 2: 二つ目") {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

func TestModelAvailabilityExplainsMissingConfiguration(t *testing.T) {
	var cfg Config
	cfg.Language = "ja-JP"
	models := configuredModels(cfg)
	for _, model := range models {
		if model.Available || model.Reason == "" {
			t.Errorf("missing reason for %s: %+v", model.ID, model)
		}
	}
	cfg.OpenAI.APIKey = "test-key"
	models = configuredModels(cfg)
	if !models[0].Available {
		t.Fatalf("OpenAI should be selectable: %+v", models[0])
	}
	cfg.OpenAI.BaseURL = "http://example.com/v1"
	models = configuredModels(cfg)
	if models[0].Available || !strings.Contains(models[0].Reason, "base_url") {
		t.Fatalf("invalid URL should be explained: %+v", models[0])
	}
}

func TestAzureEndpointWithTrailingSlashIsAvailable(t *testing.T) {
	var cfg Config
	cfg.Language = "ja-JP"
	cfg.AzureSpeech.APIKey = "azure-key"
	cfg.AzureSpeech.Endpoint = "https://speech.example.com/"
	models := configuredModels(cfg)
	for _, model := range models {
		if strings.HasPrefix(model.ID, "azure-") && !model.Available {
			t.Errorf("Azure model should be selectable: %+v", model)
		}
	}
}

func TestLanguageIsRequiredAndNormalized(t *testing.T) {
	for _, input := range []string{"", "ja", "english", "en-123"} {
		if _, err := normalizeLanguage(input); err == nil {
			t.Errorf("accepted invalid language %q", input)
		}
	}
	got, err := normalizeLanguage(" JA-jp ")
	if err != nil || got != "ja-JP" {
		t.Fatalf("normalized language = %q, %v", got, err)
	}
}

func TestAPIErrorBodyIsLoggedWithoutCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"Grant roles/serviceusage.serviceUsageConsumer to use project example-project. Bearer test-token","status":"PERMISSION_DENIED","details":[{"reason":"USER_PROJECT_DENIED","api_key":"secret-in-response","content":"audio-in-response"}]}}`))
	}))
	defer server.Close()
	root := t.TempDir()
	a := NewApp()
	a.debug.configure(true, filepath.Join(root, "asr-studio.preferences.json"))
	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	_, err = a.do(req)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("API error = %v", err)
	}
	log, err := os.ReadFile(filepath.Join(root, "logs", "asr-studio.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"roles/serviceusage.serviceUsageConsumer", "USER_PROJECT_DENIED", "PERMISSION_DENIED"} {
		if !strings.Contains(string(log), expected) {
			t.Errorf("log does not contain %q", expected)
		}
	}
	for _, secret := range []string{"test-token", "secret-in-response", "audio-in-response"} {
		if strings.Contains(string(log), secret) {
			t.Errorf("log contains sensitive value %q", secret)
		}
	}
}
