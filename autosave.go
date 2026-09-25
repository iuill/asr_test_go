package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const maxTranscriptSize = 10 * 1024 * 1024

var autoSaveModels = map[string]bool{
	"gpt-transcribe": true, "gpt-live-transcribe": true,
	"azure-speech": true, "azure-speech-diarize": true,
	"google-v1": true, "google-chirp-3": true,
}

// Each recording has its own directory; a model file is created on its first result.
func (a *App) BeginAutoSave() (string, error) {
	configFile, err := configPath()
	if err != nil {
		return "", err
	}
	return a.beginAutoSaveAt(filepath.Join(filepath.Dir(configFile), "transcripts"))
}

func (a *App) beginAutoSaveAt(root string) (string, error) {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoDir != "" {
		return a.autoDir, nil
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", fmt.Errorf("自動保存フォルダを作成できません: %w", err)
	}
	dir, err := os.MkdirTemp(root, "recording-"+time.Now().Format("20060102-150405")+"-*")
	if err != nil {
		return "", fmt.Errorf("自動保存フォルダを作成できません: %w", err)
	}
	a.autoDir = dir
	return dir, nil
}

func (a *App) AppendAutoSave(modelID, text string) (string, error) {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoDir == "" {
		return "", errors.New("自動保存が開始されていません")
	}
	if !autoSaveModels[modelID] {
		return "", errors.New("自動保存モデルが不正です")
	}
	if text == "" {
		return "", errors.New("空の文字起こし結果は保存できません")
	}
	path := filepath.Join(a.autoDir, modelID+".txt")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return "", fmt.Errorf("文字起こしを自動保存できません: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	line := []byte(text + "\n")
	if info.Size()+int64(len(line)) > maxTranscriptSize {
		return "", errors.New("保存するテキストが大きすぎます")
	}
	if _, err := file.Write(line); err != nil {
		return "", fmt.Errorf("文字起こしを自動保存できません: %w", err)
	}
	return path, nil
}

func (a *App) EndAutoSave() (string, error) {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	dir := a.autoDir
	a.autoDir = ""
	return dir, nil
}
