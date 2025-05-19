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
	FillerResponseWorker *workers.FillerResponseWorker
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

	// streamingChannel: AgentWorker output -> AgentResponseWorker input
	streamingChannel := make(chan string, 10)
	// transcriptionChannel: DeepgramClient output -> AgentWorker input
	transcriptionChannel := make(chan string)
	// fillerResponseInputChannel: DeepgramClient output -> FillerResponseWorker input
	fillerResponseInputChannel := make(chan string)
	// fillerResponseOutputChannel: FillerResponseWorker output (not used in this Call struct)
	fillerResponseOutputChannel := make(chan string)
	// outputChannel: AgentResponseWorker output -> OutputWorker input
	outputChannel := make(chan string)
	// audioChannel: StartRecievingAudio output -> DeepgramClient input
	audioChannel := make(chan []byte)
	// done: signal channel for graceful shutdown
	done := make(chan struct{})

	deepgramClient, err1 := stt.NewDeepgramClient(deepgramApiKey, transcriptionChannel, fillerResponseInputChannel)
	if err1 != nil {
		return nil, err1
	}
	agentWorker, err2 := workers.NewAgentWorker(openaiApiKey, "gpt-4o-mini", streamingChannel, transcriptionChannel)
	if err2 != nil {
		return nil, err2
	}
	log.Println("Agent worker created")
	agentResponseWorker, err3 := workers.NewAgentResponseWorker(elevenLabsApiKey, "cjVigY5qzO86Huf0OWal", "eleven_multilingual_v1", streamingChannel, outputChannel)
	if err3 != nil {
		return nil, err3
	}
	log.Println("Agent response worker created")
	fillerResponseWorker, err4 := workers.NewFillerResponseWorker(openaiApiKey, "gpt-4o-mini", fillerResponseOutputChannel, fillerResponseInputChannel)
	if err4 != nil {
		return nil, err4
	}
	log.Println("Filler response worker created")

	return &Call{
		streamSid:            "",
		ws:                   ws,
		AgentWorker:          agentWorker,
		AgentResponseWorker:  agentResponseWorker,
		FillerResponseWorker: fillerResponseWorker,
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

func (c *Call) StartOutputWorker(){
	c.OutputWorker.Start()
}

// CleanupResources gracefully releases all resources associated with the Call instance.
func (c *Call) CleanupResources() {
	// Signal all goroutines to stop first
	if c.done != nil {
		select {
		case <-c.done:
			// Channel already closed
		default:
			close(c.done)
		}
	}

	// Stop workers in reverse order of creation
	if c.OutputWorker != nil {
		c.OutputWorker.Stop()
	}

	if c.AgentResponseWorker != nil {
		c.AgentResponseWorker.Stop()
	}

	if c.AgentWorker != nil {
		c.AgentWorker.Stop()
	}

	// Close Deepgram client before closing channels
	if c.DeepgramClient != nil {
		c.DeepgramClient.Close()
	}

	// Close channels safely
	if c.StreamingChannel != nil {
		select {
		case <-c.StreamingChannel:
			// Channel already closed
		default:
			close(c.StreamingChannel)
		}
	}
	if c.OutputChannel != nil {
		select {
		case <-c.OutputChannel:
			// Channel already closed
		default:
			close(c.OutputChannel)
		}
	}
	if c.TranscriptionChannel != nil {
		select {
		case <-c.TranscriptionChannel:
			// Channel already closed
		default:
			close(c.TranscriptionChannel)
		}
	}
	if c.AudioChannel != nil {
		select {
		case <-c.AudioChannel:
			// Channel already closed
		default:
			close(c.AudioChannel)
		}
	}

	// Close WebSocket connection last
	if c.ws != nil {
		c.ws.Close()
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
			c.StartOutputWorker()
			c.SendCallOpeningMessage()
			log.Printf("Call opening message sent")

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

	c.FillerResponseWorker.Start()

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

func (c *Call) SendCallOpeningMessage(){
	c.StreamingChannel <- "Hello, how can I help you today?"
}
