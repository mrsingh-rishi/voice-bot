package llm

import (
	"context"
	"log"
	"regexp"
	"strings"

	"github.com/sashabaranov/go-openai"
)

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
func (c *OpenAIClient) StreamResponse(input string) {
	log.Printf("Sending input to OpenAI: %s\n", input)
	// Append the user input to the chat history
	c.Messages = append(c.Messages, openai.ChatCompletionMessage{
		Role:    "user",
		Content: input,
	})
	// Include system instructions along with the user input
	req := openai.ChatCompletionRequest{
		Model:    c.Model,
		Messages: c.Messages,
		Stream:   true, // Enable streaming
	}

	stream, err := c.Client.CreateChatCompletionStream(context.Background(), req)
	if err != nil {
		log.Printf("Failed to stream OpenAI response: %v\n", err)
		return
	}
	defer stream.Close()

	// log.Println("Streaming response from OpenAI...")

	// Stream the response chunks
	var completeSentence string
	for {
		response, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				log.Println("OpenAI response streaming completed")
			} else {
				log.Printf("Error receiving OpenAI response: %v\n", err)
			}
			break
		}

		// Extract and log the streamed text
		chunk := response.Choices[0].Delta.Content
		completeSentence += chunk
		if chunk != "" {
			// log.Printf("OpenAI streamed chunk: %s\n", chunk)
			// Trigger TTS callback with the streamed text chunk
			if IsCompleteSentence(chunk) {
				// send complete sentence to channel
				// log.Printf("Complete sentence: %s\n", completeSentence)
				log.Printf("Bot text: %s\n", completeSentence)
				log.Printf("Sending complete sentence to streaming channel ")
				resp := splitByPunctuation(completeSentence)
				for _, chunk := range resp {
					log.Printf("Sending chunk to streaming channel: %s\n", chunk)
					c.StreamingChannel<-chunk
				}
				completeSentence = ""
			}

		}
	}
}
// IsCompleteSentence checks if a string ends with a sentence-ending punctuation
func IsCompleteSentence(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasSuffix(trimmed, ".") ||
		strings.HasSuffix(trimmed, "!") ||
		strings.HasSuffix(trimmed, "?")
}

func splitByPunctuation(s string) []string {
	// Split on all common English punctuation marks and preserve them
	re := regexp.MustCompile(`([,.;:!?])`)
	parts := re.Split(s, -1)
	punctuation := re.FindAllString(s, -1)

	var result []string
	for i, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			if i < len(punctuation) {
				result = append(result, trimmed+punctuation[i])
			} else {
				result = append(result, trimmed)
			}
		}
	}
	return result
}
