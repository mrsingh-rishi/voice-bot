package workers

import (
	"context"
	"log"

	"github.com/mrsingh-rishi/voice-bot/tts"
)

type AgentResponseWorker struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	StreamingChannel    <-chan string
	OutputDeviceChannel chan<- string
	TTSClient           tts.ElevenLabsClient
}

func NewAgentResponseWorker(apikey string, voiceId string, modelId string, streamingChannel <-chan string, outputDeviceChannel chan<- string) (*AgentResponseWorker, error) {
	client, err := tts.NewElevenLabsClient(apikey, voiceId, modelId, outputDeviceChannel)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	agentResponseWorker := &AgentResponseWorker{
		ctx:                 ctx,
		cancel:              cancel,
		StreamingChannel:    streamingChannel,
		OutputDeviceChannel: outputDeviceChannel,
		TTSClient:           *client,
	}
	return agentResponseWorker, nil
}

func (w *AgentResponseWorker) Start() error {
	log.Println("AgentResponseWorker started")

	go func() {
		for {
			select{
				case <-w.ctx.Done():
					log.Println("AgentResponseWorker context done, exiting...")
					return
				case response := <- w.StreamingChannel: 
					if response == "" {
						log.Println("Received empty response, skipping...")
						continue
					}
					log.Printf("Received response: %s\n", response)
					// Send the response to the TTS client
					if err := w.TTSClient.GenerateSpeech(response); err != nil {
						log.Printf("Error streaming response: %v\n", err)
						continue
					}
					// Send the audio data to the output device channel	
			}
		}
	}()

	return nil
}

// Stop signals Start() to exit.
func (w *AgentResponseWorker) Stop() {
	w.cancel()
}
