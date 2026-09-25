package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const maxTranscriptSize = 10 * 1024 * 1024

// BeginAutoSave creates one file for the current recording. Each new recording
// gets a distinct name so an earlier transcript is never overwritten.
func (a *App) BeginAutoSave() (string, error) {
	configFile, err := configPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(configFile), "transcripts")
	return a.beginAutoSaveAt(dir)
}

func (a *App) beginAutoSaveAt(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("自動保存フォルダを作成できません: %w", err)
	}
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoPath != "" {
		return a.autoPath, nil
	}
	file, err := os.CreateTemp(dir, "transcript-"+time.Now().Format("20060102-150405")+"-*.txt")
	if err != nil {
		return "", fmt.Errorf("自動保存ファイルを作成できません: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	a.autoPath = file.Name()
	return a.autoPath, nil
}

func (a *App) WriteAutoSave(content string) (string, error) {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	return a.writeAutoSaveLocked(content)
}

func (a *App) writeAutoSaveLocked(content string) (string, error) {
	if len(content) > maxTranscriptSize {
		return "", errors.New("保存するテキストが大きすぎます")
	}
	if a.autoPath == "" {
		return "", errors.New("自動保存が開始されていません")
	}
	if err := writeFileAtomically(a.autoPath, []byte(content)); err != nil {
		return "", fmt.Errorf("文字起こしを自動保存できません: %w", err)
	}
	return a.autoPath, nil
}

func (a *App) EndAutoSave(content string) (string, error) {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	path, err := a.writeAutoSaveLocked(content)
	a.autoPath = ""
	return path, err
}
