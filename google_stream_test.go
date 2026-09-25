package main

import (
	"io"
	"reflect"
	"testing"

	v1pb "cloud.google.com/go/speech/apiv1/speechpb"
	v2pb "cloud.google.com/go/speech/apiv2/speechpb"
)

type fakeGoogleV1Receiver struct {
	responses []*v1pb.StreamingRecognizeResponse
}

func (f *fakeGoogleV1Receiver) Recv() (*v1pb.StreamingRecognizeResponse, error) {
	if len(f.responses) == 0 {
		return nil, io.EOF
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

type fakeGoogleV2Receiver struct {
	responses []*v2pb.StreamingRecognizeResponse
}

func (f *fakeGoogleV2Receiver) Recv() (*v2pb.StreamingRecognizeResponse, error) {
	if len(f.responses) == 0 {
		return nil, io.EOF
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestGoogleV1StreamReturnsAllFinalTranscripts(t *testing.T) {
	a := NewApp()
	session := &googleStreamSession{
		modelID: "google-v1", turnID: "r1-t1", done: make(chan struct{}),
		cancel: func() {}, close: func() error { return nil }, closeSend: func() error { return nil },
	}
	a.googleStreams = map[string]*googleStreamSession{googleStreamKey(session.modelID, session.turnID): session}
	go a.readGoogleV1(session, &fakeGoogleV1Receiver{responses: []*v1pb.StreamingRecognizeResponse{
		{Results: []*v1pb.StreamingRecognitionResult{{Alternatives: []*v1pb.SpeechRecognitionAlternative{{Transcript: "途中"}}}}},
		{Results: []*v1pb.StreamingRecognitionResult{{IsFinal: true, Alternatives: []*v1pb.SpeechRecognitionAlternative{{Transcript: "確定"}}}}},
		{Results: []*v1pb.StreamingRecognitionResult{{IsFinal: true, Alternatives: []*v1pb.SpeechRecognitionAlternative{{Transcript: "次の発話"}}}}},
	}})
	text, err := a.EndGoogleStream(session.modelID, session.turnID)
	if err != nil || !reflect.DeepEqual(text, []string{"確定", "次の発話"}) || session.interims != 1 || session.finals != 2 {
		t.Fatalf("final transcript = %q, interim=%d final=%d, %v", text, session.interims, session.finals, err)
	}
}

func TestGoogleChirpStreamReturnsAllFinalTranscripts(t *testing.T) {
	a := NewApp()
	session := &googleStreamSession{
		modelID: "google-chirp-3", turnID: "r1-t2", done: make(chan struct{}),
		cancel: func() {}, close: func() error { return nil }, closeSend: func() error { return nil },
	}
	a.googleStreams = map[string]*googleStreamSession{googleStreamKey(session.modelID, session.turnID): session}
	go a.readGoogleV2(session, &fakeGoogleV2Receiver{responses: []*v2pb.StreamingRecognizeResponse{
		{Results: []*v2pb.StreamingRecognitionResult{{Alternatives: []*v2pb.SpeechRecognitionAlternative{{Transcript: "途中"}}}}},
		{Results: []*v2pb.StreamingRecognitionResult{{IsFinal: true, Alternatives: []*v2pb.SpeechRecognitionAlternative{{Transcript: "確定"}}}}},
		{Results: []*v2pb.StreamingRecognitionResult{{IsFinal: true, Alternatives: []*v2pb.SpeechRecognitionAlternative{{Transcript: "次の発話"}}}}},
	}})
	text, err := a.EndGoogleStream(session.modelID, session.turnID)
	if err != nil || !reflect.DeepEqual(text, []string{"確定", "次の発話"}) || session.interims != 1 || session.finals != 2 {
		t.Fatalf("final transcript = %q, interim=%d final=%d, %v", text, session.interims, session.finals, err)
	}
}
