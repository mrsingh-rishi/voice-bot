package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"

	gws "github.com/gorilla/websocket"
)

// DeepgramClient wraps a websocket connection to Deepgram's realtime API.
// It accepts raw audio frames on SendAudio() and emits final transcriptions
// to the provided channels. The client supports broadcasting final results to
// multiple channels so different parts of the application can react
// independently (main transcript processing, filler-word generator, and a
// transcription-control channel used for interruption decisions).
type DeepgramClient struct {
	ctx        context.Context
	Cancel     context.CancelFunc
	Connection *gws.Conn
	APIKey     string
	Endpoint   string
	// TranscriptionChannel: main processing channel (AgentWorker input)
	TranscriptionChannel chan string
	// TranscriptionChannel2: filler response worker input
	TranscriptionChannel2 chan string
	// TranscriptionChannel3: control channel for the transcription worker
	TranscriptionChannel3 chan string
	closeOnce             sync.Once
	writeMu               sync.Mutex
}

// TranscriptionMessage models a subset of Deepgram's JSON response that we
// care about: whether the chunk is final and the top alternative text.
type TranscriptionMessage struct {
	IsFinal bool `json:"is_final"`
	Channel struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
}

// NewDeepgramClient connects to Deepgram and returns a client that will
// broadcast final transcriptions to the supplied channels. If a channel is
// nil, that subscriber will be skipped.
func NewDeepgramClient(apikey string, transcriptionChannel chan string, transcriptionChannel2 chan string, transcriptionChannel3 chan string) (*DeepgramClient, error) {
	// For now we use a fixed Deepgram listen URL and config parameters.
	dgURL := "wss://api.deepgram.com/v1/listen?model=nova-3&encoding=mulaw&sample_rate=8000&channels=1&language=multi&punctuate=true&smart_format=true&vad_events=true"

	header := http.Header{
		"Authorization": {fmt.Sprintf("Token %s", apikey)},
	}
	dgConn, _, err := gws.DefaultDialer.Dial(dgURL, header)
	if err != nil {
		log.Printf("❌ Deepgram dial error: %v", err)
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	log.Printf("✅ Connected to Deepgram")
	return &DeepgramClient{
		ctx:                   ctx,
		Cancel:                cancel,
		Connection:            dgConn,
		APIKey:                apikey,
		Endpoint:              dgURL,
		TranscriptionChannel:  transcriptionChannel,
		TranscriptionChannel2: transcriptionChannel2,
		TranscriptionChannel3: transcriptionChannel3,
	}, nil
}

// SendAudio reads raw audio frames from audioChannel and writes them to the
// Deepgram WebSocket. It also listens for Deepgram responses and processes
// final transcriptions, broadcasting them to configured channels.
func (dg *DeepgramClient) SendAudio(audioChannel <-chan []byte) {
	// Writer goroutine: send binary audio frames to Deepgram
	go func() {
		for {
			select {
			case <-dg.ctx.Done():
				return
			case audio := <-audioChannel:
				if len(audio) == 0 {
					continue
				}
				if dg.Connection == nil {
					log.Println("❌ Deepgram connection is nil")
					return
				}
				if err := dg.Connection.WriteMessage(gws.BinaryMessage, audio); err != nil {
					log.Printf("❌ Deepgram write error: %v", err)
					return
				}
			}
		}
	}()

	// Reader goroutine: read JSON messages from Deepgram and process them
	go func() {
		for {
			select {
			case <-dg.ctx.Done():
				return
			default:
				_, message, err := dg.Connection.ReadMessage()
				if err != nil {
					log.Printf("Error reading response from Deepgram: %v\n", err)
					continue
				}

				// Try to parse message as an array of responses first (some DG
				// messages come as arrays), otherwise parse as single object.
				var arrayResp []TranscriptionMessage
				if err := json.Unmarshal(message, &arrayResp); err == nil {
					for _, resp := range arrayResp {
						dg.processTranscription(resp)
					}
					continue
				}

				var singleResp TranscriptionMessage
				if err := json.Unmarshal(message, &singleResp); err != nil {
					log.Printf("Error parsing Deepgram response: %v\n", err)
					continue
				}
				dg.processTranscription(singleResp)
			}
		}
	}()
}

// processTranscription extracts the top alternative text and, for final
// results, broadcasts it to all configured transcription channels.
func (dg *DeepgramClient) processTranscription(resp TranscriptionMessage) {
	if len(resp.Channel.Alternatives) > 0 {
		text := resp.Channel.Alternatives[0].Transcript
		if text != "" && resp.IsFinal {
			select {
			case <-dg.ctx.Done():
				return
			default:
				// broadcast to configured channels if present
				if dg.TranscriptionChannel != nil {
					dg.TranscriptionChannel <- text
				}
				if dg.TranscriptionChannel2 != nil {
					dg.TranscriptionChannel2 <- text
				}
				if dg.TranscriptionChannel3 != nil {
					dg.TranscriptionChannel3 <- text
				}
			}
		}
	}
}

// Close closes the Deepgram WebSocket connection and cancels internal goroutines.
func (dg *DeepgramClient) Close() error {
	dg.Cancel()                  // signal goroutines to stop
	return dg.Connection.Close() // immedately tear down socket
}
