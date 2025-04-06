package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mrsingh-rishi/voice-bot/model"
	"github.com/mrsingh-rishi/voice-bot/queue"
)

type DeepgramClient struct {
	APIKey string
}

// NewDeepgramClient initializes a new DeepgramClient.
func NewDeepgramClient(apiKey string) *DeepgramClient {
	//TODO: initialize the Deepgram client with the API key and other necessary configurations.
	return &DeepgramClient{APIKey: apiKey}
}

// TranscriberWorker is responsible for converting audio to text using Deepgram.
type TranscriberWorker struct {
	deepgram    *DeepgramClient
	InputQueue  *queue.Queue[model.AudioChunk]
	OutputQueue *queue.Queue[model.TranscribedText]
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewTranscriberWorker initializes a new TranscriberWorker with the given Deepgram API key.
func NewTranscriberWorker(apiKey string) *TranscriberWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &TranscriberWorker{
		deepgram:    NewDeepgramClient(apiKey),
		InputQueue:  queue.New[model.AudioChunk](),
		OutputQueue: queue.New[model.TranscribedText](),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins the worker's processing loop in its own goroutine.
func (tw *TranscriberWorker) Start() {
	go tw.process()
}

// process continuously polls the input queue, processes audio chunks,
// and pushes transcribed text into the output queue.
func (tw *TranscriberWorker) process() {
	for {
		select {
		case <-tw.ctx.Done():
			log.Println("TranscriberWorker: Shutting down")
			return
		default:
			// Check if there is an item in the input queue.
			if tw.InputQueue.Len() == 0 {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Dequeue an audio chunk.
			audioChunk, ok := tw.InputQueue.Dequeue()
			if !ok {
				continue
			}

			// Process the audio chunk using the simulated Deepgram API.
			transcribed := tw.transcribe(audioChunk)

			// Enqueue the transcribed text.
			tw.OutputQueue.Enqueue(transcribed)
		}
	}
}

// transcribe simulates transcription of an audio chunk.
// Replace this with an actual call to Deepgram's API.
func (tw *TranscriberWorker) transcribe(audioChunk model.AudioChunk) model.TranscribedText {

	// TODO: Implement the actual transcription logic using Deepgram's API.

	// For demonstration, we just return a formatted string.
	transcribed := model.TranscribedText(fmt.Sprintf("Deepgram transcription of: %s", string(audioChunk)))
	log.Printf("TranscriberWorker: %s", transcribed)
	return transcribed
}

// Stop terminates the processing loop.
func (tw *TranscriberWorker) Stop() {
	tw.cancel()
}
