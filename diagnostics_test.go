package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugLoggerDisabledDoesNotCreateLogs(t *testing.T) {
	root := t.TempDir()
	var logger debugLogger
	logger.configure(false, filepath.Join(root, "asr-studio.preferences.json"))
	logger.logf("TRACE", "microphone started")
	if _, err := os.Stat(filepath.Join(root, "logs")); !os.IsNotExist(err) {
		t.Fatalf("logs directory should not exist: %v", err)
	}
}

func TestDebugLoggerRotatesThreeFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "logs", "asr-studio.log")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{path: strings.Repeat("a", maxLogSize), path + ".1": "previous", path + ".2": "oldest"} {
		if err := os.WriteFile(name, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var logger debugLogger
	logger.configure(true, filepath.Join(root, "asr-studio.preferences.json"))
	logger.logf("ERROR", "api response status=400")
	for _, name := range []string{path, path + ".1", path + ".2"} {
		info, err := os.Stat(name)
		if err != nil || info.Size() > maxLogSize {
			t.Fatalf("invalid log file %s: %v", name, err)
		}
	}
	current, _ := os.ReadFile(path)
	previous, _ := os.ReadFile(path + ".1")
	oldest, _ := os.ReadFile(path + ".2")
	if !strings.Contains(string(current), "api response status=400") || len(previous) != maxLogSize || string(oldest) != "previous" {
		t.Fatal("log rotation did not keep the newest three generations")
	}
}

func TestSafeLogCodeDoesNotRecordArbitraryText(t *testing.T) {
	if got := safeLogCode("invalid_request_error.missing_model"); got != "invalid_request_error.missing_model" {
		t.Fatalf("error code = %q", got)
	}
	if got := safeLogCode("key sk-secret123"); got != "unknown" {
		t.Fatalf("sensitive text was retained: %q", got)
	}
}

func TestSafeStreamErrorMessageKeepsPermissionReasonWithoutToken(t *testing.T) {
	message := safeStreamErrorMessage("permission denied: grant roles/serviceusage.serviceUsageConsumer\nBearer secret-token")
	if !strings.Contains(message, "roles/serviceusage.serviceUsageConsumer") || strings.Contains(message, "secret-token") || strings.Contains(message, "\n") {
		t.Fatalf("unsafe stream error message: %q", message)
	}
}
