package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoSaveCreatesDistinctFilesAndReplacesSnapshot(t *testing.T) {
	a := NewApp()
	dir := filepath.Join(t.TempDir(), "transcripts")
	first, err := a.beginAutoSaveAt(dir)
	if err != nil || filepath.Dir(first) != dir {
		t.Fatalf("begin auto save = %q, %v", first, err)
	}
	if _, err := a.WriteAutoSave("GPT Live\n最初の結果"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EndAutoSave("GPT Live\n最初の結果\n次の結果"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(first)
	if err != nil || string(data) != "GPT Live\n最初の結果\n次の結果" {
		t.Fatalf("saved transcript = %q, %v", data, err)
	}
	second, err := a.beginAutoSaveAt(dir)
	if err != nil || second == first {
		t.Fatalf("second auto save = %q, first = %q, %v", second, first, err)
	}
	if !strings.HasSuffix(second, ".txt") {
		t.Fatalf("unexpected file name: %q", second)
	}
}

func TestAutoSaveRequiresSession(t *testing.T) {
	a := NewApp()
	if _, err := a.WriteAutoSave("text"); err == nil {
		t.Fatal("write without session succeeded")
	}
}

func TestFailedFinalSavePreservesSnapshotAndReleasesSession(t *testing.T) {
	a := NewApp()
	dir := t.TempDir()
	path, err := a.beginAutoSaveAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.WriteAutoSave("saved"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.EndAutoSave(strings.Repeat("x", maxTranscriptSize+1)); err == nil {
		t.Fatal("oversized final save succeeded")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "saved" {
		t.Fatalf("previous snapshot = %q, %v", data, err)
	}
	next, err := a.beginAutoSaveAt(dir)
	if err != nil || next == path {
		t.Fatalf("next session = %q, %v", next, err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".asr-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files = %v, %v", leftovers, err)
	}
}
