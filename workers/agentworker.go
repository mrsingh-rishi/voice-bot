package workers

import (
	"context"
	"fmt"
	"log"

	"github.com/mrsingh-rishi/voice-bot/llm"
)

type AgentWorker struct {
	ctx context.Context
	cancel context.CancelFunc
	OpenAIClient      llm.OpenAIClient
	StreamingChannel  chan<- string
	TranscriptChannel <-chan string
	FillerResponseGenerator llm.OpenAIClient
	// TODO: Add other fields like ActionChannel, FillerResponse Generator, ActionWorker, etc.
}


func NewAgentWorker(apikey string, model string, streamingChannel chan <- string, transcriptionChannel <-chan string) (*AgentWorker, error){
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
	fillerPrompt := `You are given a user’s spontaneous utterance as the next input plus the following list of common human filler‑words:

uh, um, er, erm, ah, oh, uh‑huh, hmm, mm, mmm, yeah, yep, y’know, like, well, so, actually, basically, literally, seriously, honestly, frankly, admittedly, anyway, anyways, sort of, kind of, you see, I mean, let’s see, if you will, as it were, now, then, meanwhile, perhaps, maybe, you know, right, okay, gotcha, sure, alright, wow, oops, sigh, gasp, hmm‑mm, oh‑no, ah‑ha

Your task is to choose the single filler‑word from that list that a person would most likely utter immediately after the given input. Return **only** that one word—no punctuation, no extra text. Do not include examples, explanations, or formatting—just the bare filler‑word.`
	// Create OpenAI client and FillerResponseGenerator
	client, err1 := llm.NewOpenAIClient(apikey, "You are a helpful assistant.", model, streamingChannel) // System instructions will be updated later
	fillerResponseGenerator, err2 := llm.NewOpenAIClient(apikey, fillerPrompt, model, streamingChannel)
	if err1 != nil {
		return nil, err1
	}
	if err2 != nil {
		return nil, err2
	}
	if client == nil {
		return nil, err1
	}
	if fillerResponseGenerator == nil {
		return nil, err2
	}
	ctx, cancel := context.WithCancel(context.Background())
	agentWorker := &AgentWorker{
		ctx: ctx,
		cancel: cancel,
		OpenAIClient:     *client,
		StreamingChannel: streamingChannel,
		TranscriptChannel: transcriptionChannel,
		FillerResponseGenerator: *fillerResponseGenerator,
	}

	return agentWorker, nil
}

func (aw *AgentWorker) Start() {
    go func() {
        for {
            select {
            case <-aw.ctx.Done():
                // context cancelled → exit
                return

            case transcript, ok := <-aw.TranscriptChannel:
                if !ok {
                    // upstream closed → exit
                    return
                }
				log.Print("Received transcript: ", transcript)
                // first generate a human‑like filler word
                go aw.FillerResponseGenerator.StreamResponse(transcript)

                // then generate the actual assistant response
                go aw.OpenAIClient.StreamResponse(transcript)
            }
        }
    }()
}

func (aw *AgentWorker) Stop() {
	// Stop the agent worker
	aw.cancel()
}