package worker

import (
	"context"
	"log"
	"time"

	"github.com/mrsingh-rishi/voice-bot/model"
	"github.com/mrsingh-rishi/voice-bot/queue"
	"github.com/mrsingh-rishi/voice-bot/service/stt"
)

// TranscriberWorker is responsible for converting audio to text using Deepgram.
type TranscriberWorker struct {
	dgClient    *stt.DeepgramClient
	InputQueue  *queue.Queue[model.AudioChunk]
	OutputQueue *queue.Queue[model.TranscribedText]
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewTranscriberWorker initializes a new TranscriberWorker with the given Deepgram API key.
// It creates a Deepgram client (from the stt package) that is configured to push transcription
// responses into its TranscriptionChannel. The worker then converts these responses and pushes
// them into its own output queue.
func NewTranscriberWorker(apiKey string, logger *log.Logger) *TranscriberWorker {
	ctx, cancel := context.WithCancel(context.Background())

	// Create a generic queue for Deepgram transcription responses (as strings).
	dgOutputQueue := queue.New[string]()

	// Initialize the Deepgram client with the API key, the output queue, and logger.
	dgClient, err := stt.NewDeepgramClient(apiKey, dgOutputQueue, logger)
	if err != nil {
		logger.Fatalf("Failed to initialize Deepgram client: %v", err)
	}

	// Create the worker's own output queue for transcribed text.
	workerOutputQueue := queue.New[model.TranscribedText]()

	worker := &TranscriberWorker{
		dgClient:    dgClient,
		InputQueue:  queue.New[model.AudioChunk](),
		OutputQueue: workerOutputQueue,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Launch a goroutine that continuously reads from the Deepgram client's TranscriptionChannel
	// and pushes each transcription (converted to model.TranscribedText) into the worker's output queue.
	go func() {
		for {
			select {
			case transcript, ok := <-dgClient.TranscriptionChannel:
				if !ok {
					return
				}
				worker.OutputQueue.Enqueue(model.TranscribedText(transcript))
			case <-ctx.Done():
				return
			}
		}
	}()

	return worker
}

// Start begins the worker's processing loop in its own goroutine.
func (tw *TranscriberWorker) Start() {
	go tw.process()
}

// process continuously polls the input queue for audio chunks. For each chunk,
// it calls the Deepgram client's Process method to send the chunk over the WebSocket.
// The Deepgram client (running in its own goroutines) will process the chunk and
// eventually push the transcription response into its TranscriptionChannel.
func (tw *TranscriberWorker) process() {
	for {
		select {
		case <-tw.ctx.Done():
			log.Println("TranscriberWorker: Shutting down")
			return
		default:
			if tw.InputQueue.Len() == 0 {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// Dequeue an audio chunk from the input queue.
			audioChunk, ok := tw.InputQueue.Dequeue()
			if !ok {
				continue
			}
			// Process the audio chunk using the Deepgram client.
			tw.dgClient.Process(audioChunk)
		}
	}
}

// Stop terminates the processing loop and closes the Deepgram WebSocket connection.
func (tw *TranscriberWorker) Stop() {
	tw.cancel()
	tw.dgClient.Close()
}
