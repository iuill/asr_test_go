package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestLiveURL(t *testing.T) {
	got, err := liveURL("https://api.openai.com/v1/")
	if err != nil || got != "wss://api.openai.com/v1/realtime?intent=transcription" {
		t.Fatalf("URL = %q, %v", got, err)
	}
	if _, err := liveURL("http://example.com/v1"); err == nil {
		t.Fatal("insecure URL accepted")
	}
}

func TestLiveTranscriptionProtocol(t *testing.T) {
	observed := make(chan []map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		events := make([]map[string]any, 0, 3)
		for i := 0; i < 3; i++ {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var event map[string]any
			if json.Unmarshal(data, &event) != nil {
				return
			}
			events = append(events, event)
		}
		observed <- events
		_ = conn.WriteJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "item-1", "transcript": "こんにちは"})
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	session := &liveSession{conn: conn, readDone: make(chan struct{}), settled: make(chan struct{})}
	close(session.settled)
	a := NewApp()
	a.debug.configure(true, filepath.Join(t.TempDir(), "asr-studio.preferences.json"))
	a.live = session
	go a.readLive(session)
	if err := session.write(liveUpdate("en")); err != nil {
		t.Fatal(err)
	}
	if err := a.AppendLive(base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 3})); err != nil {
		t.Fatal(err)
	}
	if err := a.CommitLive(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.settled:
	case <-time.After(2 * time.Second):
		t.Fatal("transcription did not complete")
	}
	events := <-observed
	if events[0]["type"] != "session.update" || events[1]["type"] != "input_audio_buffer.append" || events[2]["type"] != "input_audio_buffer.commit" {
		t.Fatalf("events = %#v", events)
	}
	sessionConfig := events[0]["session"].(map[string]any)
	input := sessionConfig["audio"].(map[string]any)["input"].(map[string]any)
	if input["turn_detection"] != nil || input["transcription"].(map[string]any)["model"] != "gpt-live-transcribe" || input["format"].(map[string]any)["rate"] != float64(24000) {
		t.Fatalf("session config = %#v", input)
	}
	languages := input["transcription"].(map[string]any)["languages"].([]any)
	if len(languages) != 1 || languages[0] != "en" {
		t.Fatalf("language hint = %#v", languages)
	}
	if err := a.StopLive(); err != nil {
		t.Fatal(err)
	}
	<-session.readDone
	_, logPath, _ := a.debug.status()
	logData, err := os.ReadFile(logPath)
	if err != nil || strings.Contains(string(logData), "ERROR") {
		t.Fatalf("normal shutdown logged an error: %q, %v", logData, err)
	}
}
