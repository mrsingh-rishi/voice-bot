package workers

import (
	"context"
	"fmt"
	"log"

	"github.com/mrsingh-rishi/voice-bot/types"
)


type TranscriptionWorker struct {
	ctx                     context.Context
	cancel                  context.CancelFunc
	TranscriptionOutputChannel chan<- string
	TranscriptionInputChannel  <-chan types.TranscriptionChannel
	BroadcasterOutputChannel  chan<- bool
}


func NewTranscriptionWorker(transcriptionOutputChannel chan<- string, transcriptionInputChannel <-chan types.TranscriptionChannel, broadcasterOutputChannel chan<- bool) (*TranscriptionWorker, error) {
	// Params Validation
	if transcriptionOutputChannel == nil {
		return nil, fmt.Errorf("transcription channel is required")
	}
	if transcriptionInputChannel == nil {	
		return nil, fmt.Errorf("transcription input channel is required")
	}
	if broadcasterOutputChannel == nil {
		return nil, fmt.Errorf("broadcaster output channel is required")
	}
	// Create context and cancel function
	ctx, cancel := context.WithCancel(context.Background())
	// Create TranscriptionWorker
	transcriptionWorker := &TranscriptionWorker{
		ctx:                     ctx,
		cancel:                  cancel,
		TranscriptionOutputChannel: transcriptionOutputChannel,
		TranscriptionInputChannel:  transcriptionInputChannel,
		BroadcasterOutputChannel:  broadcasterOutputChannel,
	}
	return transcriptionWorker, nil
}


func (tw *TranscriptionWorker) Start() {
	go func() {
		for {
			select {
			case <-tw.ctx.Done():
				return
			case transcription := <-tw.TranscriptionInputChannel:
				if transcription.Final {
					log.Printf("Got Final Transcription: %s, Confidence: %f", transcription.Transcription, transcription.Confidence)
					tw.TranscriptionOutputChannel <- transcription.Transcription
				} else {
					log.Printf("Got Partial Transcription: %s, Confidence: %f", transcription.Transcription, transcription.Confidence)
					
				}
			}
		}
	}()
}