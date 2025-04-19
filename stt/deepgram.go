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
	Connection *gws.Conn
	APIKey      string
	Endpoint	string
	TranscriptionChannel chan TranscriptionChannel
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
			Transcript string `json:"transcript"`
			Confidence float64 `json:"confidence"`
		} `json:"alternatives"`
	} `json:"channel"`
}

// for now we will use default deepgram config
func NewDeepgramClient(apikey string ) *DeepgramClient {
	dgURL := "wss://api.deepgram.com/v1/listen?model=nova-2-phonecall&encoding=mulaw&sample_rate=8000&channels=1&language=en-US&punctuate=true&smart_format=true&vad_events=true"

	header := http.Header{
		"Authorization": {fmt.Sprintf("Token %s", apikey)},
	}
	dgConn, _, err := gws.DefaultDialer.Dial(dgURL, header)
	if err != nil {
		log.Printf("❌ Deepgram dial error: %v", err)
		return nil
	}

	log.Printf("✅ Connected to Deepgram")
	return &DeepgramClient{
		Connection: dgConn,
		APIKey:     apikey,
		Endpoint:   dgURL,
		TranscriptionChannel: make(chan TranscriptionChannel),
	}
}

func (dg *DeepgramClient) SendAudio(ctx context.Context, audioChannel <-chan []byte) {
	go func(){
		for{
			select {
			case audio := <-audioChannel:
				{
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
				log.Printf("✅ Sent audio to Deepgram")
			}
			case <-ctx.Done():
				log.Println("Transcription session ended")
				return
			}
		}
	}()

	go func() {
		for {
			_, message, err := dg.Connection.ReadMessage()
			if err != nil {
				log.Printf("Error reading response from Deepgram: %v\n", err)
				return
			}

			// Debug raw Deepgram response
			log.Printf("Raw Deepgram response: %s\n", string(message))

			var transcription TranscriptionMessage
			if err := json.Unmarshal(message, &transcription); err != nil {
				log.Printf("Error parsing Deepgram response: %v\n", err)
				continue
			}

			// Extract and log the transcription
			if len(transcription.Channel.Alternatives) > 0 {
				text := transcription.Channel.Alternatives[0].Transcript
				if text != "" {
					dg.TranscriptionChannel <- TranscriptionChannel{
						Transcription: text,
						Confidence:    transcription.Channel.Alternatives[0].Confidence,
						Final:         transcription.IsFinal,
					}
					log.Printf("Transcription: %s\n", text)
				} else {
					log.Println("No transcription received")
				}
			}
		}
	}()
}

// Close closes the Deepgram WebSocket connection
func (dg *DeepgramClient) Close() error {
	if err := dg.Connection.WriteMessage(gws.CloseMessage, gws.FormatCloseMessage(gws.CloseNormalClosure, "Closing connection")); err != nil {
		return err
	}
	return dg.Connection.Close()
}