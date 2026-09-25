package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreferencesPreserveLoggingWhenTimestampsChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asr-studio.preferences.json")
	if err := os.WriteFile(path, []byte(`{"enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	pref, err := readPreferences(path)
	if err != nil || !pref.Enabled || pref.ShowTimestamps || pref.AutoSave {
		t.Fatalf("old preferences = %+v, %v", pref, err)
	}
	pref.ShowTimestamps = true
	pref.AutoSave = true
	if err := savePreferences(path, pref); err != nil {
		t.Fatal(err)
	}
	pref, err = readPreferences(path)
	if err != nil || !pref.Enabled || !pref.ShowTimestamps || !pref.AutoSave {
		t.Fatalf("saved preferences = %+v, %v", pref, err)
	}
}
