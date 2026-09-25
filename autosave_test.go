package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoSaveAppendsToSeparateModelFiles(t *testing.T) {
	a := NewApp()
	root := filepath.Join(t.TempDir(), "transcripts")
	first, err := a.beginAutoSaveAt(root)
	if err != nil || filepath.Dir(first) != root {
		t.Fatalf("begin = %q, %v", first, err)
	}
	for _, item := range []struct{ model, text string }{
		{"google-v1", "最初"}, {"google-chirp-3", "別モデル"}, {"google-v1", "次の発話"},
	} {
		if _, err := a.AppendAutoSave(item.model, item.text); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Join(first, "google-v1.txt"))
	if err != nil || string(got) != "最初\n次の発話\n" {
		t.Fatalf("V1 transcript = %q, %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(first, "google-chirp-3.txt"))
	if err != nil || string(got) != "別モデル\n" {
		t.Fatalf("Chirp transcript = %q, %v", got, err)
	}
	if ended, err := a.EndAutoSave(); err != nil || ended != first {
		t.Fatalf("end = %q, %v", ended, err)
	}
	second, err := a.beginAutoSaveAt(root)
	if err != nil || second == first {
		t.Fatalf("next recording = %q, first = %q, %v", second, first, err)
	}
}

func TestAutoSaveValidatesSessionModelAndSize(t *testing.T) {
	a := NewApp()
	if _, err := a.AppendAutoSave("google-v1", "text"); err == nil {
		t.Fatal("append without session succeeded")
	}
	dir, err := a.beginAutoSaveAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AppendAutoSave("../outside", "secret"); err == nil {
		t.Fatal("invalid model succeeded")
	}
	if _, err := a.AppendAutoSave("google-v1", "saved"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AppendAutoSave("google-v1", strings.Repeat("x", maxTranscriptSize)); err == nil {
		t.Fatal("oversized append succeeded")
	}
	got, err := os.ReadFile(filepath.Join(dir, "google-v1.txt"))
	if err != nil || string(got) != "saved\n" {
		t.Fatalf("previous results = %q, %v", got, err)
	}
	if _, err := a.EndAutoSave(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AppendAutoSave("google-v1", "after end"); err == nil {
		t.Fatal("append after end succeeded")
	}
}
