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
	"strings"
	"time"

	"golang.org/x/oauth2/google"
)

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
		endpoint = fmt.Sprintf("https://%s-speech.googleapis.com/v2/projects/%s/locations/%s/recognizers/_:recognize", location, url.PathEscape(g.ProjectID), location)
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
