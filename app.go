package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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
	if len(content) > maxTranscriptSize {
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
	if err := writeFileAtomically(path, []byte(content)); err != nil {
		return false, err
	}
	return true, nil
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
	var logErr error
	info.DebugLogging, info.LogPath, logErr = a.debug.status()
	if logErr != nil {
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
