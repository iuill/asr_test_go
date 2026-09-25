package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	speechv1 "cloud.google.com/go/speech/apiv1"
	v1pb "cloud.google.com/go/speech/apiv1/speechpb"
	speechv2 "cloud.google.com/go/speech/apiv2"
	v2pb "cloud.google.com/go/speech/apiv2/speechpb"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"google.golang.org/api/option"
	"google.golang.org/grpc/status"
)

type googleStreamSession struct {
	modelID    string
	turnID     string
	cancel     context.CancelFunc
	close      func() error
	closeSend  func() error
	send       func([]byte) error
	writeMu    sync.Mutex
	done       chan struct{}
	err        error
	finalText  []string
	audioBytes int
	interims   int
	finals     int
}

type googleStreamEvent struct {
	Type    string `json:"type"`
	ModelID string `json:"model_id"`
	TurnID  string `json:"turn_id"`
	Index   int    `json:"index"`
	Text    string `json:"text,omitempty"`
	Message string `json:"message,omitempty"`
}

type googleV1Receiver interface {
	Recv() (*v1pb.StreamingRecognizeResponse, error)
}

type googleV2Receiver interface {
	Recv() (*v2pb.StreamingRecognizeResponse, error)
}

var googleTurnIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{1,80}$`)

func googleStreamKey(modelID, turnID string) string { return modelID + "/" + turnID }

func (a *App) StartGoogleStream(modelID, turnID string) error {
	if (modelID != "google-v1" && modelID != "google-chirp-3") || !googleTurnIDPattern.MatchString(turnID) {
		return errors.New("Googleストリームの指定が不正です")
	}
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	if len(cfg.Google.ServiceAccount) < 3 || cfg.Google.ProjectID == "" {
		return errors.New("Googleサービスアカウントとproject_idが未設定です")
	}
	key := googleStreamKey(modelID, turnID)
	a.googleMu.Lock()
	if a.googleStreams == nil {
		a.googleStreams = make(map[string]*googleStreamSession)
	}
	if _, exists := a.googleStreams[key]; exists {
		a.googleMu.Unlock()
		return errors.New("Googleストリームは既に開始しています")
	}
	a.googleMu.Unlock()
	ctx, cancel := context.WithCancel(a.ctx)
	session := &googleStreamSession{modelID: modelID, turnID: turnID, cancel: cancel, done: make(chan struct{})}
	if modelID == "google-v1" {
		err = a.startGoogleV1(ctx, cfg, session)
	} else {
		err = a.startGoogleV2(ctx, cfg, session)
	}
	if err != nil {
		cancel()
		if session.close != nil {
			_ = session.close()
		}
		a.debug.logf("ERROR", "google stream start failed model=%s code=%s message=%s", modelID, status.Code(err), safeStreamErrorMessage(err.Error()))
		return err
	}
	a.googleMu.Lock()
	if _, exists := a.googleStreams[key]; exists {
		a.googleMu.Unlock()
		cancel()
		_ = session.close()
		return errors.New("Googleストリームは既に開始しています")
	}
	a.googleStreams[key] = session
	a.googleMu.Unlock()
	a.debug.logf("INFO", "google stream started model=%s turn=%s", modelID, turnID)
	return nil
}

func (a *App) startGoogleV1(ctx context.Context, cfg Config, session *googleStreamSession) error {
	client, err := speechv1.NewClient(ctx, option.WithCredentialsJSON(cfg.Google.ServiceAccount), option.WithQuotaProject(cfg.Google.ProjectID))
	if err != nil {
		return err
	}
	session.close = client.Close
	stream, err := client.StreamingRecognize(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&v1pb.StreamingRecognizeRequest{StreamingRequest: &v1pb.StreamingRecognizeRequest_StreamingConfig{StreamingConfig: &v1pb.StreamingRecognitionConfig{
		Config:         &v1pb.RecognitionConfig{Encoding: v1pb.RecognitionConfig_LINEAR16, SampleRateHertz: 16000, LanguageCode: cfg.Language},
		InterimResults: true,
	}}}); err != nil {
		return err
	}
	session.send = func(audio []byte) error {
		return stream.Send(&v1pb.StreamingRecognizeRequest{StreamingRequest: &v1pb.StreamingRecognizeRequest_AudioContent{AudioContent: audio}})
	}
	session.closeSend = stream.CloseSend
	go a.readGoogleV1(session, stream)
	return nil
}

func (a *App) startGoogleV2(ctx context.Context, cfg Config, session *googleStreamSession) error {
	location := cfg.Google.Location
	if location == "" {
		location = "us"
	}
	if location != "us" && location != "eu" {
		return errors.New("Chirp 3 のlocationは us または eu にしてください")
	}
	client, err := speechv2.NewClient(ctx, option.WithCredentialsJSON(cfg.Google.ServiceAccount), option.WithQuotaProject(cfg.Google.ProjectID), option.WithEndpoint(location+"-speech.googleapis.com:443"))
	if err != nil {
		return err
	}
	session.close = client.Close
	stream, err := client.StreamingRecognize(ctx)
	if err != nil {
		return err
	}
	config := &v2pb.StreamingRecognitionConfig{
		Config: &v2pb.RecognitionConfig{
			DecodingConfig: &v2pb.RecognitionConfig_ExplicitDecodingConfig{ExplicitDecodingConfig: &v2pb.ExplicitDecodingConfig{
				Encoding: v2pb.ExplicitDecodingConfig_LINEAR16, SampleRateHertz: 16000, AudioChannelCount: 1,
			}},
			Model: "chirp_3", LanguageCodes: []string{cfg.Language},
		},
		StreamingFeatures: &v2pb.StreamingRecognitionFeatures{InterimResults: true},
	}
	if err := stream.Send(&v2pb.StreamingRecognizeRequest{
		Recognizer:       fmt.Sprintf("projects/%s/locations/%s/recognizers/_", cfg.Google.ProjectID, location),
		StreamingRequest: &v2pb.StreamingRecognizeRequest_StreamingConfig{StreamingConfig: config},
	}); err != nil {
		return err
	}
	session.send = func(audio []byte) error {
		return stream.Send(&v2pb.StreamingRecognizeRequest{StreamingRequest: &v2pb.StreamingRecognizeRequest_Audio{Audio: audio}})
	}
	session.closeSend = stream.CloseSend
	go a.readGoogleV2(session, stream)
	return nil
}

func (a *App) AppendGoogleStream(modelID, turnID, audioBase64 string) error {
	if len(audioBase64) == 0 || len(audioBase64) > 128*1024 {
		return errors.New("Google音声チャンクが不正です")
	}
	audio, err := base64.StdEncoding.DecodeString(audioBase64)
	if err != nil {
		return errors.New("Google音声チャンクを読み取れません")
	}
	a.googleMu.Lock()
	session := a.googleStreams[googleStreamKey(modelID, turnID)]
	a.googleMu.Unlock()
	if session == nil {
		return errors.New("Googleストリームが開始されていません")
	}
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	if err := session.send(audio); err != nil {
		return err
	}
	session.audioBytes += len(audio)
	return nil
}

func (a *App) EndGoogleStream(modelID, turnID string) ([]string, error) {
	key := googleStreamKey(modelID, turnID)
	a.googleMu.Lock()
	session := a.googleStreams[key]
	delete(a.googleStreams, key)
	a.googleMu.Unlock()
	if session == nil {
		return nil, nil
	}
	defer session.cancel()
	defer session.close()
	session.writeMu.Lock()
	err := session.closeSend()
	session.writeMu.Unlock()
	if err != nil {
		session.cancel()
		<-session.done
		return nil, err
	}
	select {
	case <-session.done:
		a.debug.logf("INFO", "google stream completed model=%s turn=%s audio_bytes=%d interim_results=%d final_results=%d", modelID, turnID, session.audioBytes, session.interims, session.finals)
		return session.finalText, session.err
	case <-time.After(15 * time.Second):
		session.cancel()
		<-session.done
		return nil, errors.New("Googleストリームの確定結果がタイムアウトしました")
	}
}

func (a *App) readGoogleV1(session *googleStreamSession, stream googleV1Receiver) {
	defer close(session.done)
	defer a.emitGoogle(googleStreamEvent{Type: "ended", ModelID: session.modelID, TurnID: session.turnID})
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			a.googleStreamError(session, err)
			return
		}
		if response.GetError() != nil {
			a.googleStreamError(session, errors.New(response.GetError().Message))
			return
		}
		partial := []string{}
		for _, result := range response.GetResults() {
			if len(result.GetAlternatives()) == 0 {
				continue
			}
			text := strings.TrimSpace(result.GetAlternatives()[0].GetTranscript())
			if result.GetIsFinal() {
				session.finals++
				if text != "" {
					session.finalText = append(session.finalText, text)
					a.emitGoogle(googleStreamEvent{Type: "completed", ModelID: session.modelID, TurnID: session.turnID, Index: len(session.finalText) - 1, Text: text})
				}
			} else if text != "" {
				session.interims++
				partial = append(partial, text)
			}
		}
		if len(partial) > 0 {
			a.emitGoogle(googleStreamEvent{Type: "partial", ModelID: session.modelID, TurnID: session.turnID, Text: strings.Join(partial, " ")})
		}
	}
}

func (a *App) readGoogleV2(session *googleStreamSession, stream googleV2Receiver) {
	defer close(session.done)
	defer a.emitGoogle(googleStreamEvent{Type: "ended", ModelID: session.modelID, TurnID: session.turnID})
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			a.googleStreamError(session, err)
			return
		}
		partial := []string{}
		for _, result := range response.GetResults() {
			if len(result.GetAlternatives()) == 0 {
				continue
			}
			text := strings.TrimSpace(result.GetAlternatives()[0].GetTranscript())
			if result.GetIsFinal() {
				session.finals++
				if text != "" {
					session.finalText = append(session.finalText, text)
					a.emitGoogle(googleStreamEvent{Type: "completed", ModelID: session.modelID, TurnID: session.turnID, Index: len(session.finalText) - 1, Text: text})
				}
			} else if text != "" {
				session.interims++
				partial = append(partial, text)
			}
		}
		if len(partial) > 0 {
			a.emitGoogle(googleStreamEvent{Type: "partial", ModelID: session.modelID, TurnID: session.turnID, Text: strings.Join(partial, " ")})
		}
	}
}

func (a *App) googleStreamError(session *googleStreamSession, err error) {
	session.err = err
	a.debug.logf("ERROR", "google stream failed model=%s turn=%s code=%s message=%s", session.modelID, session.turnID, status.Code(err), safeStreamErrorMessage(err.Error()))
	a.emitGoogle(googleStreamEvent{Type: "error", ModelID: session.modelID, TurnID: session.turnID, Message: err.Error()})
}

func (a *App) emitGoogle(event googleStreamEvent) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "google-transcript", event)
	}
}
