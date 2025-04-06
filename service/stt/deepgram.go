package stt

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/gorilla/websocket"
	"github.com/mrsingh-rishi/voice-bot/queue"
)

// TranscriptionMessage represents the JSON response from Deepgram.
type TranscriptionMessage struct {
	Channel struct {
		Alternatives []struct {
			Transcript string `json:"transcript"`
		} `json:"alternatives"`
	} `json:"channel"`
}

// DeepgramClient encapsulates the Deepgram WebSocket connection and output queue.
type DeepgramClient struct {
	Connection  *websocket.Conn
	APIKey      string
	Endpoint    string
	Logger      *log.Logger
	OutputQueue *queue.Queue[string]
	TranscriptionChannel chan string
}

// NewDeepgramClient initializes a new Deepgram client using the API key and provided output queue.
func NewDeepgramClient(apiKey string, outputQueue *queue.Queue[string], logger *log.Logger) (*DeepgramClient, error) {
	// The endpoint URL is configured with various parameters.
	url := "wss://api.deepgram.com/v1/listen?model=nova-2-phonecall&encoding=mulaw&sample_rate=8000&channels=1&language=en-US&punctuate=true&smart_format=true&vad_events=true"

	// Dial the Deepgram WebSocket endpoint with the API key as the Authorization header.
	conn, _, err := websocket.DefaultDialer.Dial(url, map[string][]string{
		"Authorization": {fmt.Sprintf("Token %s", apiKey)},
	})
	if err != nil {
		logger.Printf("Failed to connect to Deepgram WebSocket: %v\n", err)
		return nil, err
	}

	logger.Println("Connected to Deepgram WebSocket successfully")
	client := &DeepgramClient{
		Connection:           conn,
		APIKey:               apiKey,
		Endpoint:             url,
		Logger:               logger,
		OutputQueue:          outputQueue,
		TranscriptionChannel: make(chan string),
	}

	// Start listening for responses in a background goroutine.
	go client.listenForResponses()

	return client, nil
}

// Process sends the provided audio chunk to Deepgram.
// This method does not return a transcription immediately; instead, transcription responses
// are processed in the background and pushed into the output queue.
func (dg *DeepgramClient) Process(audioChunk []byte) {
	if len(audioChunk) == 0 {
		dg.Logger.Println("Received empty audio chunk, skipping.")
		return
	}

	// Send the audio chunk as a binary message over the WebSocket.
	if err := dg.Connection.WriteMessage(websocket.BinaryMessage, audioChunk); err != nil {
		dg.Logger.Printf("Error sending audio chunk: %v\n", err)
		return
	}

	dg.Logger.Printf("Audio chunk sent successfully, length: %d bytes", len(audioChunk))
}

// listenForResponses continuously reads messages from the Deepgram WebSocket.
// For each response, it parses the transcription and pushes it into the output queue.
func (dg *DeepgramClient) listenForResponses() {
	for {
		_, message, err := dg.Connection.ReadMessage()
		if err != nil {
			dg.Logger.Printf("Error reading response from Deepgram: %v\n", err)
			return
		}

		// Log the raw Deepgram response for debugging purposes.
		dg.Logger.Printf("Raw Deepgram response: %s", string(message))

		var transcription TranscriptionMessage
		if err := json.Unmarshal(message, &transcription); err != nil {
			dg.Logger.Printf("Error parsing Deepgram response: %v\n", err)
			continue
		}

		// If there is a transcription alternative, push it into the output queue.
		if len(transcription.Channel.Alternatives) > 0 {
			text := transcription.Channel.Alternatives[0].Transcript
			if text != "" {
				dg.OutputQueue.Enqueue(text)
				dg.Logger.Printf("Enqueued transcription: %s", text)
			} else {
				dg.Logger.Println("Deepgram response contained an empty transcript.")
			}
		} else {
			dg.Logger.Println("No transcription alternatives found in Deepgram response.")
		}
	}
}

// Close gracefully closes the Deepgram WebSocket connection.
func (dg *DeepgramClient) Close() error {
	// Send a normal closure message to the server.
	if err := dg.Connection.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Closing connection")); err != nil {
		return err
	}
	return dg.Connection.Close()
}
