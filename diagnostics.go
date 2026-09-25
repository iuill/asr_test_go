package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxLogSize = 5 * 1024 * 1024

type debugLogger struct {
	mu      sync.Mutex
	enabled bool
	path    string
	lastErr error
}

func (l *debugLogger) configure(enabled bool, configFile string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	path := filepath.Join(filepath.Dir(configFile), "logs", "asr-studio.log")
	if !enabled {
		l.enabled = false
		l.path = path
		l.lastErr = nil
		return
	}
	if l.enabled && l.path == path && l.lastErr == nil {
		return
	}
	l.enabled = true
	l.path = path
	l.lastErr = l.writeLocked("INFO", "debug logging enabled")
}

func (l *debugLogger) status() (bool, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled, l.path, l.lastErr
}

func (l *debugLogger) logf(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled {
		return
	}
	l.lastErr = l.writeLocked(level, fmt.Sprintf(format, args...))
}

func (l *debugLogger) writeLocked(level, message string) error {
	line := fmt.Sprintf("%s %-5s %s\n", time.Now().Format(time.RFC3339Nano), level, message)
	if len(line) > maxLogSize {
		return errors.New("ログ行が大きすぎます")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0700); err != nil {
		return err
	}
	if info, err := os.Stat(l.path); err == nil {
		if info.Size()+int64(len(line)) > maxLogSize {
			if err := rotateLogs(l.path); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(line)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func safeModelID(id string) string {
	switch id {
	case "gpt-transcribe", "gpt-live-transcribe", "azure-speech", "azure-speech-diarize", "google-v1", "google-chirp-3":
		return id
	default:
		return "unknown"
	}
}

func safeLogCode(code string) string {
	if len(code) == 0 || len(code) > 80 {
		return "unknown"
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && r != '_' && r != '.' {
			return "unknown"
		}
	}
	return code
}

func rotateLogs(path string) error {
	if err := os.Remove(path + ".2"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(path+".1", path+".2"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(path, path+".1")
}
