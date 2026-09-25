package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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
	l.lastErr = l.writeLocked("INFO", "========== DEBUG LOGGING ENABLED ==========")
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

const maxAPIErrorLogLength = 4096

var longEncodedValue = regexp.MustCompile(`[A-Za-z0-9_+/=-]{100,}`)
var bearerValue = regexp.MustCompile(`(?i)bearer\s+[^\s"']+`)

func safeStreamErrorMessage(message string) string {
	message = bearerValue.ReplaceAllString(message, "Bearer [redacted]")
	message = longEncodedValue.ReplaceAllString(message, "[redacted]")
	if len(message) > maxAPIErrorLogLength {
		message = message[:maxAPIErrorLogLength]
	}
	return strconv.Quote(message)
}

// Keep provider error details useful for diagnosis without writing credentials,
// request payloads, or unbounded/multiline responses to the debug log.
func safeAPIErrorBody(body []byte, req *http.Request) string {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return "[non-JSON API error body omitted]"
	}
	secrets := []string{}
	for _, name := range []string{"Authorization", "Ocp-Apim-Subscription-Key", "X-Goog-Api-Key"} {
		if header := req.Header.Get(name); header != "" {
			secrets = append(secrets, header)
			if strings.HasPrefix(strings.ToLower(header), "bearer ") {
				secrets = append(secrets, header[7:])
			}
		}
	}
	value = sanitizeAPIErrorValue(value, secrets)
	encoded, err := json.Marshal(value)
	if err != nil {
		return "[API error body could not be encoded]"
	}
	if len(encoded) > maxAPIErrorLogLength {
		const suffix = "[truncated]"
		limit := maxAPIErrorLogLength - len(suffix)
		for !utf8.RuneStart(encoded[limit]) {
			limit--
		}
		return string(encoded[:limit]) + suffix
	}
	return string(encoded)
}

func sanitizeAPIErrorValue(value any, secrets []string) any {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "key") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") || strings.Contains(lower, "authorization") || lower == "audio" || lower == "content" || lower == "transcript" || lower == "payload" {
				v[key] = "[redacted]"
				continue
			}
			v[key] = sanitizeAPIErrorValue(child, secrets)
		}
		return v
	case []any:
		for i, child := range v {
			v[i] = sanitizeAPIErrorValue(child, secrets)
		}
		return v
	case string:
		for _, secret := range secrets {
			v = strings.ReplaceAll(v, secret, "[redacted]")
		}
		v = longEncodedValue.ReplaceAllString(v, "[redacted]")
		return v
	default:
		return value
	}
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
