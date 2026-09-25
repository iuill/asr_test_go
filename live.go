package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type liveSession struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   int
	settled   chan struct{}
	readDone  chan struct{}
}

type liveEvent struct {
	Type       string `json:"type"`
	ItemID     string `json:"item_id,omitempty"`
	Delta      string `json:"delta,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Message    string `json:"message,omitempty"`
}

func liveURL(base string) (string, error) {
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	base = strings.TrimRight(base, "/")
	if !validHTTPSURL(base, false) {
		return "", errors.New("openai.base_url はHTTPS URLにしてください")
	}
	u, _ := url.Parse(base)
	u.Scheme = "wss"
	u.Path = strings.TrimRight(u.Path, "/") + "/realtime"
	u.RawQuery = "intent=transcription"
	return u.String(), nil
}

func (a *App) StartLive() error {
	a.debug.logf("TRACE", "live connection starting")
	a.liveMu.Lock()
	defer a.liveMu.Unlock()
	if a.live != nil {
		return errors.New("Liveセッションは既に開始しています")
	}
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return errors.New("openai.api_key が空です")
	}
	endpoint, err := liveURL(cfg.OpenAI.BaseURL)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, endpoint, http.Header{"Authorization": []string{"Bearer " + cfg.OpenAI.APIKey}})
	if err != nil {
		if resp != nil {
			a.debug.logf("ERROR", "live handshake failed status=%d", resp.StatusCode)
			return fmt.Errorf("Live接続に失敗しました (HTTP %d): %w", resp.StatusCode, err)
		}
		a.debug.logf("ERROR", "live connection failed error_type=%T", err)
		return fmt.Errorf("Live接続に失敗しました: %w", err)
	}
	session := &liveSession{conn: conn, readDone: make(chan struct{}), settled: make(chan struct{})}
	close(session.settled)
	conn.SetReadLimit(1 << 20)
	if err := session.write(liveUpdate(openAILanguage(cfg))); err != nil {
		a.debug.logf("ERROR", "live session update failed error_type=%T", err)
		conn.Close()
		return fmt.Errorf("Liveセッションを設定できません: %w", err)
	}
	a.live = session
	a.debug.logf("INFO", "live connection established")
	go a.readLive(session)
	return nil
}

func liveUpdate(language string) map[string]any {
	return map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{"input": map[string]any{
				"format":         map[string]any{"type": "audio/pcm", "rate": 24000},
				"transcription":  map[string]any{"model": "gpt-live-transcribe", "languages": []string{language}, "delay": "low"},
				"turn_detection": nil,
			}},
		},
	}
}

func (s *liveSession) write(event any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return s.conn.WriteJSON(event)
}

func (a *App) AppendLive(audioBase64 string) error {
	if len(audioBase64) == 0 || len(audioBase64) > 128*1024 {
		return errors.New("Live音声チャンクが不正です")
	}
	if _, err := base64.StdEncoding.DecodeString(audioBase64); err != nil {
		return errors.New("Live音声チャンクを読み取れません")
	}
	a.liveMu.Lock()
	session := a.live
	a.liveMu.Unlock()
	if session == nil {
		return errors.New("Liveセッションが開始されていません")
	}
	return session.write(map[string]any{"type": "input_audio_buffer.append", "audio": audioBase64})
}

func (a *App) CommitLive() error {
	a.debug.logf("TRACE", "live audio turn committed")
	a.liveMu.Lock()
	session := a.live
	a.liveMu.Unlock()
	if session == nil {
		return errors.New("Liveセッションが開始されていません")
	}
	session.pendingMu.Lock()
	if session.pending == 0 {
		session.settled = make(chan struct{})
	}
	session.pending++
	session.pendingMu.Unlock()
	if err := session.write(map[string]any{"type": "input_audio_buffer.commit"}); err != nil {
		session.finishPending()
		return err
	}
	return nil
}

func (s *liveSession) finishPending() {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if s.pending > 0 {
		s.pending--
		if s.pending == 0 {
			close(s.settled)
		}
	}
}

func (a *App) StopLive() error {
	a.debug.logf("INFO", "live connection stopping")
	a.liveMu.Lock()
	session := a.live
	a.live = nil
	a.liveMu.Unlock()
	if session == nil {
		return nil
	}
	session.pendingMu.Lock()
	settled := session.settled
	session.pendingMu.Unlock()
	select {
	case <-settled:
	case <-session.readDone:
	case <-time.After(10 * time.Second):
	}
	return session.conn.Close()
}

func (a *App) shutdown(context.Context) {
	a.debug.logf("INFO", "app stopped")
	a.liveMu.Lock()
	session := a.live
	a.live = nil
	a.liveMu.Unlock()
	if session != nil {
		_ = session.conn.Close()
	}
}

func (a *App) readLive(session *liveSession) {
	defer close(session.readDone)
	for {
		_, data, err := session.conn.ReadMessage()
		if err != nil {
			a.liveMu.Lock()
			current := a.live == session
			if current {
				a.live = nil
			}
			a.liveMu.Unlock()
			if current {
				if closeErr, ok := err.(*websocket.CloseError); ok {
					a.debug.logf("ERROR", "live connection closed code=%d reason=%s", closeErr.Code, safeLogCode(closeErr.Text))
				} else {
					a.debug.logf("ERROR", "live read failed error_type=%T", err)
				}
				a.emitLive(liveEvent{Type: "error", Message: "Live接続が切れました: " + err.Error()})
			}
			return
		}
		var envelope struct {
			Type       string `json:"type"`
			ItemID     string `json:"item_id"`
			Delta      string `json:"delta"`
			Transcript string `json:"transcript"`
			Error      struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			continue
		}
		switch envelope.Type {
		case "conversation.item.input_audio_transcription.delta":
			a.emitLive(liveEvent{Type: "delta", ItemID: envelope.ItemID, Delta: envelope.Delta})
		case "conversation.item.input_audio_transcription.completed":
			a.debug.logf("TRACE", "live transcript completed")
			a.emitLive(liveEvent{Type: "completed", ItemID: envelope.ItemID, Transcript: envelope.Transcript})
			session.finishPending()
		case "error":
			a.debug.logf("ERROR", "live server error type=%s code=%s", safeLogCode(envelope.Error.Type), safeLogCode(envelope.Error.Code))
			a.emitLive(liveEvent{Type: "error", Message: envelope.Error.Message})
		}
	}
}

func (a *App) emitLive(event liveEvent) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "live-transcript", event)
	}
}
