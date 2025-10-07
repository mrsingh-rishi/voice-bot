package workers

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/mrsingh-rishi/voice-bot/tts"
)

// AgentResponseWorker consumes sentence fragments from StreamingChannel and
// converts them to audio using the TTS client. The resulting base64 audio
// frames are emitted to OutputDeviceChannel.
type AgentResponseWorker struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	StreamingChannel    <-chan string
	OutputDeviceChannel chan<- string
	TTSClient           tts.ElevenLabsClient
	// busy is set while generating speech so the transcription worker can
	// detect whether the system is currently speaking and decide interruption.
	busy int32 // atomic flag
}

// NewAgentResponseWorker constructs the TTS response worker. The TTS client
// is given the OutputDeviceChannel so it can stream chunked audio there.
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

// Start begins the main TTS loop. Each incoming sentence is turned into
// audio synchronously. GenerateSpeech accepts a context, so Stop() will
// cancel any in-flight HTTP TTS requests.
func (w *AgentResponseWorker) Start() error {
	log.Println("AgentResponseWorker started")

	go func() {
		for {
			select {
			case <-w.ctx.Done():
				log.Println("AgentResponseWorker context done, exiting...")
				return
			case response := <-w.StreamingChannel:
				if response == "" {
					log.Println("Received empty response, skipping...")
					continue
				}
				log.Printf("Received response: %s\n", response)
				// mark busy while generating speech
				atomic.StoreInt32(&w.busy, 1)
				// Send the response to the TTS client (cancelable)
				if err := w.TTSClient.GenerateSpeech(w.ctx, response); err != nil {
					log.Printf("Error streaming response: %v\n", err)
					atomic.StoreInt32(&w.busy, 0)
					continue
				}
				atomic.StoreInt32(&w.busy, 0)
				// Send the audio data to the output device channel
			}
		}
	}()

	return nil
}

// Stop signals Start() to exit by cancelling context.
func (w *AgentResponseWorker) Stop() {
	w.cancel()
}

// IsBusy returns whether the worker is currently speaking/streaming audio.
func (w *AgentResponseWorker) IsBusy() bool {
	return atomic.LoadInt32(&w.busy) == 1
}
