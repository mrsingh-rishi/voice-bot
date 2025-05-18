package workers

import (
	"context"
	"fmt"
	"log"

	"github.com/mrsingh-rishi/voice-bot/llm"
)

type AgentWorker struct {
	ctx                     context.Context
	cancel                  context.CancelFunc
	OpenAIClient            llm.OpenAIClient
	AgentOutputChannel        chan<- string
	AgentInputChannel       <-chan string
	// TODO: Add other fields like ActionChannel, FillerResponse Generator, ActionWorker, etc.
}

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
		ctx:                     ctx,
		cancel:                  cancel,
		OpenAIClient:            *client,
		AgentOutputChannel:        streamingChannel,
		AgentInputChannel:       transcriptionChannel,
	}

	return agentWorker, nil
}

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
				// Send the transcript to the OpenAI client for processing
				aw.OpenAIClient.StreamResponse(transcript)
			}
		}
	}()
}

func (aw *AgentWorker) Stop() {
	// Stop the agent worker
	aw.cancel()
}