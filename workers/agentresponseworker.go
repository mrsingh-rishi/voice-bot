package workers

import (
	"context"

	"github.com/mrsingh-rishi/voice-bot/tts"
)

type AgentResponseWorker struct {
	ctx   context.Context
	cancel  context.CancelFunc
	StreamingChannel <-chan string
	OutputDeviceChannel chan<- string
	TTSClient tts.ElevenLabsClient
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
		ctx: ctx,
		cancel: cancel,
		StreamingChannel: streamingChannel,
		OutputDeviceChannel: outputDeviceChannel,
		TTSClient: *client,
	}
	return agentResponseWorker, nil
}

func (w *AgentResponseWorker) Start() {
    for {
        select {
        case <-w.ctx.Done():
            // we've been asked to stop
            return
        case response, ok := <-w.StreamingChannel:
            if !ok {
                // channel closed
                return
            }
            go w.TTSClient.GenerateSpeech(response)
        }
    }
}

// Stop signals Start() to exit.
func (w *AgentResponseWorker) Stop() {
    w.cancel()
}