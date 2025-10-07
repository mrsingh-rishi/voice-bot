package workers

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/mrsingh-rishi/voice-bot/llm"
)

// AgentWorker receives final transcriptions and invokes the OpenAI client
// to produce a streaming response. Sentences from the OpenAI stream are
// emitted on AgentOutputChannel.
type AgentWorker struct {
	ctx                context.Context
	cancel             context.CancelFunc
	OpenAIClient       llm.OpenAIClient
	AgentOutputChannel chan<- string
	AgentInputChannel  <-chan string
	// busy is an atomic flag indicating whether the worker is in the
	// middle of processing a transcription (1) or idle (0). The
	// transcription worker uses IsBusy() to decide interruption.
	busy int32 // atomic flag: 1 = busy, 0 = idle
}

// NewAgentWorker constructs an agent worker instance. The OpenAI client is
// created here and will stream sentence fragments into streamingChannel.
func NewAgentWorker(apikey string, model string, streamingChannel chan<- string, transcriptionChannel <-chan string) (*AgentWorker, error) {
	// Params Validation
	if apikey == "" {
		return nil, fmt.Errorf("API key is required")
	}
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if streamingChannel == nil {
		return nil, fmt.Errorf("streaming channel is required")
	}
	if transcriptionChannel == nil {
		return nil, fmt.Errorf("transcription channel is required")
	}

	// Create OpenAI client and FillerResponseGenerator
	client, err1 := llm.NewOpenAIClient(apikey, "You are a helpful assistant.", model, streamingChannel) // System instructions will be updated later
	if err1 != nil {
		return nil, err1
	}
	if client == nil {
		return nil, err1
	}
	ctx, cancel := context.WithCancel(context.Background())
	agentWorker := &AgentWorker{
		ctx:                ctx,
		cancel:             cancel,
		OpenAIClient:       *client,
		AgentOutputChannel: streamingChannel,
		AgentInputChannel:  transcriptionChannel,
	}

	return agentWorker, nil
}

// Start begins the main loop of the worker. It listens for final
// transcriptions on AgentInputChannel and sends them to OpenAI for
// streaming. The OpenAI streaming call uses the worker's context so Stop()
// can cancel in-flight requests.
func (aw *AgentWorker) Start() {
	go func() {
		log.Printf("Streaming channel AW address: %p\n", aw.AgentOutputChannel)
		for {
			select {
			case <-aw.ctx.Done():
				// context cancelled → exit
				log.Println("AgentWorker context done, exiting...")
				return

			case transcript, ok := <-aw.AgentInputChannel:
				if !ok {
					// upstream closed → exit
					return
				}
				log.Print("Received transcript: ", transcript)
				// mark busy while streaming
				atomic.StoreInt32(&aw.busy, 1)
				// Send the transcript to the OpenAI client for processing and allow cancellation
				aw.OpenAIClient.StreamResponseWithContext(aw.ctx, transcript)
				atomic.StoreInt32(&aw.busy, 0)
			}
		}
	}()
}

// Stop cancels the worker's context which will cause the Start loop to exit
// and abort any in-flight OpenAI streaming through StreamResponseWithContext.
func (aw *AgentWorker) Stop() {
	aw.cancel()
}

// IsBusy returns whether the worker is currently processing an input.
func (aw *AgentWorker) IsBusy() bool {
	return atomic.LoadInt32(&aw.busy) == 1
}
