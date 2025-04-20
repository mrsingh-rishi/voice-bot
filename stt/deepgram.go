package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	gws "github.com/gorilla/websocket"
)

type DeepgramClient struct {
	ctx        context.Context
	Cancel     context.CancelFunc
	Connection *gws.Conn
	APIKey     string
	Endpoint   string
	// TranscriptionChannel chan TranscriptionChannel
	TranscriptionChannel chan string
}

type TranscriptionChannel struct {
	Transcription string
	Confidence    float64
	Final         bool
}

type TranscriptionMessage struct {
	IsFinal bool `json:"is_final"`
	Channel struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
}

type DeepgramResponse struct {
	IsFinal bool `json:"is_final"`
	Channel []struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
}

// for now we will use default deepgram config
func NewDeepgramClient(apikey string, transcriptionChannel chan string) (*DeepgramClient, error) {
	dgURL := "wss://api.deepgram.com/v1/listen?model=nova-2-phonecall&encoding=mulaw&sample_rate=8000&channels=1&language=en-US&punctuate=true&smart_format=true&vad_events=true"

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
		ctx:                  ctx,
		Cancel:               cancel,
		Connection:           dgConn,
		APIKey:               apikey,
		Endpoint:             dgURL,
		TranscriptionChannel: transcriptionChannel,
		// TranscriptionChannel: make(chan TranscriptionChannel),
	}, nil
}

func (dg *DeepgramClient) SendAudio(audioChannel <-chan []byte) {
	go func() {
		for {
			select {
			case <-dg.ctx.Done():
				return
			case audio := <-audioChannel:
				if len(audio) == 0 {
					log.Println("Received empty audio chunk")
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

	go func() {
		for {
			select {
			case <-dg.ctx.Done():
				return
			default:
				_, message, err := dg.Connection.ReadMessage()
				if err != nil {
					log.Printf("Error reading response from Deepgram: %v\n", err)
					return
				}

				// Try to parse as array response first
				var arrayResp []TranscriptionMessage
				if err := json.Unmarshal(message, &arrayResp); err == nil {
					for _, resp := range arrayResp {
						dg.processTranscription(resp)
					}
					continue
				}

				// If array parsing fails, try as single object
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

func (dg *DeepgramClient) processTranscription(resp TranscriptionMessage) {
	if len(resp.Channel.Alternatives) > 0 {
		text := resp.Channel.Alternatives[0].Transcript
		if text != "" && resp.IsFinal {
			dg.TranscriptionChannel <- text
			log.Printf("Transcription: %s\n", text)
		}
	}
}

// Close closes the Deepgram WebSocket connection
func (dg *DeepgramClient) Close() error {
	if err := dg.Connection.WriteMessage(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, "Closing connection")); err != nil {
		return err
	}
	dg.ctx.Done()
	return dg.Connection.Close()
}
