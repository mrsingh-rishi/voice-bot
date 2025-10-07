package llm

import (
	"context"
	"log"
	"regexp"
	"strings"

	"github.com/sashabaranov/go-openai"
)

// OpenAIClient wraps the go-openai client and provides a streaming
// interface that converts incoming chunks into sentence-level messages.
// Sentences are emitted to StreamingChannel so downstream workers (TTS)
// can handle them one by one.
type OpenAIClient struct {
	Client             *openai.Client
	Messages           []openai.ChatCompletionMessage
	SystemInstructions string
	StreamingChannel   chan<- string
	Model              string      // Model to use for OpenAI API
	ActionChannel      chan string // Channel to send actions to the main thread(Type will be defined later)
}

func NewOpenAIClient(apiKey string, systemInstructions string, model string, streamingChannel chan<- string) (*OpenAIClient, error) {
	client := openai.NewClient(apiKey)
	return &OpenAIClient{
		Client:             client,
		SystemInstructions: systemInstructions,
		StreamingChannel:   streamingChannel,
		Messages: []openai.ChatCompletionMessage{
			{Role: "system", Content: systemInstructions}, // System instructions
		},
		Model: model,
		// will update it later when actions are defined
		ActionChannel: make(chan string), // Initialize the action channel
	}, nil
}

// StreamResponse sends a user query to OpenAI and streams the response in real-time
// using a background context. For cancellation support pass a context to
// StreamResponseWithContext.
func (c *OpenAIClient) StreamResponse(input string) {
	// default to background context for compatibility
	c.StreamResponseWithContext(context.Background(), input)
}

// StreamResponseWithContext streams a response using the provided context so
// the caller can cancel the request (used for interruptions). It appends the
// user message to the message history, opens a stream, and processes incoming
// deltas. The client buffers chunks and emits full sentences to
// StreamingChannel.
func (c *OpenAIClient) StreamResponseWithContext(ctx context.Context, input string) {
	log.Printf("Sending input to OpenAI: %s\n", input)
	c.Messages = append(c.Messages, openai.ChatCompletionMessage{
		Role:    "user",
		Content: input,
	})
	req := openai.ChatCompletionRequest{
		Model:    c.Model,
		Messages: c.Messages,
		Stream:   true,
	}

	stream, err := c.Client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		log.Printf("Failed to stream OpenAI response: %v\n", err)
		return
	}
	defer stream.Close()

	// prepare our buffer and sentence-matcher. We emit text sentence-by-sentence
	// to make TTS output more natural and chunked.
	sentenceRe := regexp.MustCompile(`[^\.!\?]*[\.!\?]`)
	buffer := &strings.Builder{}

	// Read and process incoming chunks
	c.readAndProcess(stream, sentenceRe, buffer)

	// Send any trailing text
	c.flushRemaining(buffer)
}

// readAndProcess: receive each chunk, collate into sentences, and emit them
func (c *OpenAIClient) readAndProcess(
	stream *openai.ChatCompletionStream,
	sentenceRe *regexp.Regexp,
	buffer *strings.Builder,
) {
	for {
		resp, err := stream.Recv()
		if err != nil {
			if err.Error() != "EOF" {
				log.Printf("Error receiving OpenAI response: %v\n", err)
			}
			break
		}
		chunk := resp.Choices[0].Delta.Content
		if chunk == "" {
			continue
		}

		// Break out complete sentences from the buffer
		sentences := processChunk(buffer, chunk, sentenceRe)
		for _, s := range sentences {
			c.StreamingChannel <- s
		}
	}
}

// processChunk: append new text, extract all full sentences, return them
func processChunk(
	buffer *strings.Builder,
	chunk string,
	sentenceRe *regexp.Regexp,
) []string {
	buffer.WriteString(chunk)
	text := buffer.String()

	var sentences []string
	for {
		loc := sentenceRe.FindStringIndex(text)
		if loc == nil {
			break
		}
		sentence := strings.TrimSpace(text[:loc[1]])
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
		text = text[loc[1]:]
	}

	// reset buffer to leftover
	buffer.Reset()
	buffer.WriteString(text)
	return sentences
}

// flushRemaining: send any leftover text at end-of-stream
func (c *OpenAIClient) flushRemaining(buffer *strings.Builder) {
	leftover := strings.TrimSpace(buffer.String())
	if leftover != "" {
		c.StreamingChannel <- leftover
	}
}
