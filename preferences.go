package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type appPreferences struct {
	Enabled        bool `json:"enabled"`
	ShowTimestamps bool `json:"show_timestamps"`
	AutoSave       bool `json:"auto_save"`
}

func preferencesPath() (string, error) {
	configFile, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(configFile), "asr-studio.preferences.json"), nil
}

func loadPreferences() (appPreferences, string, error) {
	var pref appPreferences
	path, err := preferencesPath()
	if err != nil {
		return pref, path, err
	}
	pref, err = readPreferences(path)
	return pref, path, err
}

func readPreferences(path string) (appPreferences, error) {
	var pref appPreferences
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return pref, nil
	}
	if err != nil {
		return pref, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pref); err != nil {
		return pref, fmt.Errorf("画面設定を読み取れません: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return pref, errors.New("画面設定に余分なデータがあります")
	}
	return pref, nil
}

func savePreferences(path string, pref appPreferences) error {
	data, err := json.MarshalIndent(pref, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("画面設定を保存できません: %w", err)
	}
	return nil
}

func (a *App) SetDebugLogging(enabled bool) error {
	a.prefMu.Lock()
	defer a.prefMu.Unlock()
	pref, path, err := loadPreferences()
	if err != nil {
		return err
	}
	pref.Enabled = enabled
	if err := savePreferences(path, pref); err != nil {
		return err
	}
	a.debug.configure(enabled, path)
	if _, _, err := a.debug.status(); err != nil {
		return fmt.Errorf("ログファイルを作成できません: %w", err)
	}
	return nil
}

func (a *App) SetShowTimestamps(enabled bool) error {
	a.prefMu.Lock()
	defer a.prefMu.Unlock()
	pref, path, err := loadPreferences()
	if err != nil {
		return err
	}
	pref.ShowTimestamps = enabled
	return savePreferences(path, pref)
}

func (a *App) SetAutoSave(enabled bool) error {
	a.prefMu.Lock()
	defer a.prefMu.Unlock()
	pref, path, err := loadPreferences()
	if err != nil {
		return err
	}
	pref.AutoSave = enabled
	return savePreferences(path, pref)
}
