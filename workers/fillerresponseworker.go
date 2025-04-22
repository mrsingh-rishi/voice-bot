package workers

import (
	"context"
	"fmt"

	"github.com/mrsingh-rishi/voice-bot/llm"
)

type FillerResponseWorker struct {
	ctx                     context.Context
	cancel                  context.CancelFunc
	OpenAIClient            llm.OpenAIClient
	FillerOutputChannel     chan<- string
	FillerInputChannel      <-chan string
}


func NewFillerResponseWorker(apikey string, model string, fillerOutputChannel chan<- string, fillerInputChannel <-chan string) (*FillerResponseWorker, error) {
	// Params Validation
	if apikey == "" {
		return nil, fmt.Errorf("API key is required")
	}
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if fillerOutputChannel == nil {
		return nil, fmt.Errorf("filler output channel is required")
	}
	if fillerInputChannel == nil {
		return nil, fmt.Errorf("filler input channel is required")
	}
	fillerPrompt := `You are given a user's spontaneous utterance as the next input plus the following list of common human filler‑words:

uh, um, er, erm, ah, oh, uh‑huh, hmm, mm, mmm, yeah, yep, y'know, like, well, so, actually, basically, literally, seriously, honestly, frankly, admittedly, anyway, anyways, sort of, kind of, you see, I mean, let's see, if you will, as it were, now, then, meanwhile, perhaps, maybe, you know, right, okay, gotcha, sure, alright, wow, oops, sigh, gasp, hmm‑mm, oh‑no, ah‑ha

Your task is to choose the single filler‑word from that list that a person would most likely utter immediately after the given input. Return **only** that one word—no punctuation, no extra text. Do not include examples, explanations, or formatting—just the bare filler‑word.`

	fillerResponseGenerator, err2 := llm.NewOpenAIClient(apikey, fillerPrompt, model, fillerOutputChannel)
	if err2 != nil {
		return nil, err2
	}
	if fillerResponseGenerator == nil {
		return nil, err2
	}

	ctx, cancel := context.WithCancel(context.Background())
	fillerResponseWorker := &FillerResponseWorker{
		ctx:                 ctx,
		cancel:              cancel,
		FillerOutputChannel: fillerOutputChannel,
		FillerInputChannel:  fillerInputChannel,
		OpenAIClient:        *fillerResponseGenerator,
	}

	return fillerResponseWorker, nil
}

func (frw *FillerResponseWorker) Start() {
	go func() {
		for {
			select {
			case <-frw.ctx.Done():
				return
			case input := <-frw.FillerInputChannel:
				if input == "" {
					continue
				}
				frw.OpenAIClient.StreamResponse(input)
			}
		}
	}()
}

func (frw *FillerResponseWorker) Stop() {
	frw.cancel()
}