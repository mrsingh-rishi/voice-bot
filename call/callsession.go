package call

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"os"

	"github.com/gofiber/websocket/v2"
	"github.com/mrsingh-rishi/voice-bot/output"
	"github.com/mrsingh-rishi/voice-bot/stt"
	"github.com/mrsingh-rishi/voice-bot/workers"
)

type twilioEvent struct {
	Event string `json:"event"` // "start", "media", "stop"
	Media struct {
		Payload string `json:"payload"` // base64 audio
	} `json:"media"`
	Start struct {
		// Normally contains CallSid, streamSid, etc.
		CallSid   string `json:"callSid"`
		StreamSid string `json:"streamSid"`
	} `json:"start"`
}

type Call struct {
	streamSid            string
	ws                   *websocket.Conn
	AgentWorker          *workers.AgentWorker
	AgentResponseWorker  *workers.AgentResponseWorker
	OutputWorker         *output.TwilioOutput
	StreamingChannel     chan string
	OutputChannel        chan string
	TranscriptionChannel chan string
	DeepgramClient       *stt.DeepgramClient
	AudioChannel         chan []byte
	done                 chan struct{} // Signal channel for graceful shutdown
}

func NewCall(ws *websocket.Conn) (*Call, error) {
	deepgramApiKey := os.Getenv("DEEPGRAM_API_KEY")
	openaiApiKey := os.Getenv("OPEN_AI_API_KEY")
	elevenLabsApiKey := os.Getenv("ELEVEN_LABS_API_KEY")

	if deepgramApiKey == "" || openaiApiKey == "" || elevenLabsApiKey == "" {
		return nil, errors.New("missing required environment variables")
	}

	streamingChannel := make(chan string)
	transcriptionChannel := make(chan string)
	outputChannel := make(chan string)
	audioChannel := make(chan []byte)
	done := make(chan struct{})

	deepgramClient, err1 := stt.NewDeepgramClient(deepgramApiKey, transcriptionChannel)
	if err1 != nil {
		return nil, err1
	}
	agentWorker, err2 := workers.NewAgentWorker(openaiApiKey, "gpt-4o-mini", streamingChannel, transcriptionChannel)
	if err2 != nil {
		return nil, err2
	}
	log.Println("Agent worker created")
	agentResponseWorker, err3 := workers.NewAgentResponseWorker(elevenLabsApiKey, "JBFqnCBsd6RMkjVDRZzb", "eleven_multilingual_v1", streamingChannel, outputChannel)
	if err3 != nil {
		return nil, err3
	}
	log.Println("Agent response worker created")
	return &Call{
		streamSid:            "",
		ws:                   ws,
		AgentWorker:          agentWorker,
		AgentResponseWorker:  agentResponseWorker,
		OutputWorker:         nil,
		StreamingChannel:     streamingChannel,
		OutputChannel:        outputChannel,
		TranscriptionChannel: transcriptionChannel,
		DeepgramClient:       deepgramClient,
		AudioChannel:         audioChannel,
		done:                 done,
	}, nil
}

func (c *Call) CreateOutputWorker() error {
	if c.streamSid == "" {
		c.CleanupResources()
		return errors.New("streamSid is empty")
	}

	outputWorker, err := output.NewTwilioOutput(c.streamSid, c.ws, c.OutputChannel)
	if err != nil {
		c.CleanupResources()
		return err
	}

	c.OutputWorker = outputWorker
	return nil
}

// CleanupResources gracefully releases all resources associated with the Call instance.
func (c *Call) CleanupResources() {
	if c.done != nil {
		close(c.done)
	}

	if c.ws != nil {
		c.ws.Close()
	}

	if c.OutputWorker != nil {
		c.OutputWorker.Stop()
	}

	if c.AgentWorker != nil {
		c.AgentWorker.Stop()
	}

	if c.AgentResponseWorker != nil {
		c.AgentResponseWorker.Stop()
	}

	if c.DeepgramClient != nil {
		c.DeepgramClient.Close()
	}
}

// closeChannelSafely closes a channel if it is not nil.
func closeChannelSafely(ch chan string) {
	if ch != nil {
		close(ch)
	}
}

func (c *Call) SetStreamSid(streamSid string) {
	c.streamSid = streamSid
	c.CreateOutputWorker()
}

func (c *Call) StartRecievingAudio(audioChannel chan []byte) {
	defer c.CleanupResources()

	for {
		// select {
		// case <-c.done:
		// 	return
		// default:
		// 	if c.ws == nil {
		// 		log.Println("WebSocket connection is nil")
		// 		return
		// 	}

			_, msg, err := c.ws.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Println("WebSocket closed normally:", err)
				} else {
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}

			var ev twilioEvent
			if err := json.Unmarshal(msg, &ev); err != nil {
				log.Printf("JSON unmarshal error: %v", err)
				continue
			}

			switch ev.Event {
			case "start":
				log.Printf("Stream started: CallSid=%s, StreamSid=%s", ev.Start.CallSid, ev.Start.StreamSid)
				c.SetStreamSid(ev.Start.StreamSid)

			case "media":
				chunk, err := base64.StdEncoding.DecodeString(ev.Media.Payload)
				if err != nil {
					log.Printf("Base64 decode error: %v", err)
					continue
				}
				audioChannel <- chunk
				// select {
				// case audioChannel <- chunk:
				// 	// Successfully sent chunk
				// case <-c.done:
				// 	return
				// default:
				// 	log.Println("Audio channel is full, dropping chunk")
				// }

			case "stop":
				log.Println("Stream stopped")
				return

			default:
				log.Printf("Unknown event: %s", ev.Event)
			}
		// }
	}
}

func (c *Call) Start() {
	// Start the agent worker
	c.AgentWorker.Start()
	log.Printf("Agent worker started")

	// Start the agent response worker
	c.AgentResponseWorker.Start()

	// Start receiving audio in a separate goroutine
	go func() {
		c.StartRecievingAudio(c.AudioChannel)
		log.Printf("Started receiving audio")
	}()

	// Start sending audio to Deepgram in a separate goroutine
	go func() {
		c.DeepgramClient.SendAudio(c.AudioChannel)
		log.Printf("Started sending audio to Deepgram")
	}()

	// Wait for done signal
	<-c.done
}
