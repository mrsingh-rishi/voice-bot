package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
    "context"
    "strings"
    "bytes"
    "io"
    "net/url"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	gws "github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	twilio "github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
    "github.com/sashabaranov/go-openai"
)

type callRequest struct {
    To string `json:"to"`
}

var messages = []openai.ChatCompletionMessage{}

type callResponse struct {
    SID     string `json:"sid,omitempty"`
    Message string `json:"message"`
}
type deepgramResponse struct {
    IsFinal bool `json:"is_final"`
    Channel struct {
        Alternatives []struct {
            Transcript string `json:"transcript"`
        } `json:"alternatives"`
    } `json:"channel"`
}
var streamSid string
type Role struct {
    Role string `json:"role"`
}

const (
    UserRole      = "user"
    AssistantRole = "assistant"
)

func handleDeepgramMessage(msg []byte, openAiChannel chan string) {
    // ignore empty frames
    if len(msg) == 0 {
        return
    }

    // Helper to extract & log a single response
    process := func(resp deepgramResponse) {
        if !resp.IsFinal {
            return
        }
        if len(resp.Channel.Alternatives) == 0 {
            // log.Println("⚠️  final segment but no alternatives")
            return
        }
        text := resp.Channel.Alternatives[0].Transcript
        log.Printf("📝 Final Deepgram transcript: %s", text)
        if(len(text) > 0){
            log.Printf("Sending final Transcript to open ai openAiChannel")
            openAiChannel <- text
        }
    }

    // Detect array vs. object
    switch msg[0] {
    case '[':
        // JSON array of responses
        var arr []deepgramResponse
        if err := json.Unmarshal(msg, &arr); err != nil {
            // log.Printf("❌ parse array error: %v", err)
            return
        }
        for _, resp := range arr {
            process(resp)
        }

    case '{':
        // Single JSON object
        var resp deepgramResponse
        if err := json.Unmarshal(msg, &resp); err != nil {
            // log.Printf("❌ parse object error: %v", err)
            return
        }
        process(resp)

    default:
        // log.Printf("❓ unexpected JSON prefix: %q", msg[0])
    }
}

func processText(openaiClient *openai.Client ,text string, elevenLabsChannel chan string) {
    prompt := "You are Rishi's Assistant, a virtual assistant. You are friendly and helpful. You can answer questions, provide information, and assist with tasks. Always be polite and professional."
    
    if len(messages) == 0 {
        messages = append(messages, openai.ChatCompletionMessage{
            Role:    openai.ChatMessageRoleSystem,
            Content: prompt,
        })
    }

    chatHistory := buildChatHistory(text, Role{Role: UserRole})
    req := openai.ChatCompletionRequest{
        Model: openai.GPT4oMini,
        Messages: chatHistory,
        Stream: true,
        MaxTokens: 100,
    }

    stream, err := openaiClient.CreateChatCompletionStream(context.Background(), req)

    if err != nil {
		log.Printf("Failed to stream OpenAI response: %v\n", err)
		return
	}

    defer stream.Close()

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
                log.Printf("Bot Message: ")
                log.Printf("%s", completeSentence)
				elevenLabsChannel <- completeSentence
				completeSentence = ""
			}

		}
	}

}

func IsCompleteSentence(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasSuffix(trimmed, ".") ||
		strings.HasSuffix(trimmed, "!") ||
		strings.HasSuffix(trimmed, "?")
}


func buildChatHistory(text string, role Role) []openai.ChatCompletionMessage {
    if(role.Role == UserRole) {
        messages = append(messages, openai.ChatCompletionMessage{
            Role:  openai.ChatMessageRoleUser,
            Content: text,
        })
    } else if(role.Role == AssistantRole) {
        messages = append(messages, openai.ChatCompletionMessage{
            Role:  openai.ChatMessageRoleAssistant,
            Content: text,
        })
    }

    return messages
}
func streamElevenTTS(
    apiKey   string,
    ws       *websocket.Conn,
    text     string,
    streamID string,
) error {
    log.Printf("▶️ Starting streamElevenTTS (streamID=%s)", streamID)

    // 1️⃣ Build the streaming URL with query params
    voiceID := "JBFqnCBsd6RMkjVDRZzb"
    base, _ := url.Parse(
        fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s/stream", voiceID),
    )
    // request alaw at 8kHz – adjust as needed (mp3_44100_128 is another option) 
    q := base.Query()
    q.Set("output_format", "alaw_8000")
    // q.Set("optimize_streaming_latency", "0") // deprecated :contentReference[oaicite:7]{index=7}
    base.RawQuery = q.Encode()
    log.Printf("🔗 Streaming URL: %s", base.String())

    // 2️⃣ Prepare JSON payload
    payload := map[string]interface{}{
        "text":     text,
        "model_id": "eleven_multilingual_v2",
        // "voice_settings": map[string]float64{
        //     "stability":        0.75,
        //     "similarity_boost": 0.7,
        // },
    }
    bodyBytes, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("❌ marshal payload: %w", err)
    }

    // 3️⃣ Build HTTP request
    req, err := http.NewRequest("POST", base.String(), bytes.NewReader(bodyBytes))
    if err != nil {
        return fmt.Errorf("❌ build request: %w", err)
    }
    req.Header.Set("xi-api-key", apiKey)    // :contentReference[oaicite:8]{index=8}
    req.Header.Set("Content-Type", "application/json") // :contentReference[oaicite:9]{index=9}

    // 4️⃣ Send request
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return fmt.Errorf("❌ HTTP request error: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("❌ bad status: %s", resp.Status)
    }
    log.Printf("📶 Streaming began (status %s)", resp.Status)

    // 5️⃣ Read chunked audio and emit as media events
    buf := make([]byte, 4096)
    for {
        n, readErr := resp.Body.Read(buf)
        if n > 0 {
            // Base64‑encode raw audio chunk
            b64 := base64.StdEncoding.EncodeToString(buf[:n])

            // JSON media event
            mediaMsg := map[string]interface{}{
                "event":     "media",
                "streamSid": streamID,
                "media": map[string]string{
                    "payload": b64,
                },
            }
            if err := ws.WriteJSON(mediaMsg); err != nil {
                return fmt.Errorf("❌ WS write media: %w", err)
            }
        }
        if readErr != nil {
            if readErr != io.EOF {
                return fmt.Errorf("❌ read stream: %w", readErr)
            }
            break
        }
    }
    log.Println("✅ Audio stream complete")

    // 6️⃣ Final mark event
    markMsg := map[string]interface{}{
        "event":     "mark",
        "streamSid": streamID,
        "mark": map[string]string{
            "name": "audio chunks sent",
        },
    }
    if err := ws.WriteJSON(markMsg); err != nil {
        return fmt.Errorf("❌ WS write mark: %w", err)
    }
    log.Println("🏁 Sent final mark event")

    return nil
}
// Twilio’s streaming payload
type twilioEvent struct {
    Event string `json:"event"` // "start", "media", "stop"
    Media struct {
        Payload string `json:"payload"` // base64 audio
    } `json:"media"`
    Start struct {
        // Normally contains CallSid, streamSid, etc.
        CallSid  string `json:"callSid"`
        StreamSid string `json:"streamSid"`
    } `json:"start"`
}

func main() {
    // Load .env if present
    if err := godotenv.Load(); err != nil {
        log.Println("No .env file found, falling back to environment variables")
    }

    // Twilio config
    accountSid := os.Getenv("TWILIO_ACCOUNT_SID")
    authToken  := os.Getenv("TWILIO_AUTH_TOKEN")
    fromNumber := os.Getenv("TWILIO_FROM_NUMBER")
    baseUrl    := os.Getenv("BASE_URL")
    baseWsUrl  := os.Getenv("BASE_WS_URL")
	deepgramApiKey := os.Getenv("DEEPGRAM_API_KEY")
    openaiApiKey := os.Getenv("OPEN_AI_API_KEY")
    elevenLabsApiKey := os.Getenv("ELEVEN_LABS_API_KEY")
    if accountSid == "" || authToken == "" || fromNumber == "" {
        log.Fatal("TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN and TWILIO_FROM_NUMBER must be set")
    }
    if baseWsUrl == "" {
        log.Fatal("BASE_WS_URL must be set")
    }
	if deepgramApiKey == "" {
		log.Fatal("DEEPGRAM_API_KEY must be set")
	}
	if baseUrl == "" {
		log.Fatal("BASE_URL must be set")
	}
    if openaiApiKey == "" {
        log.Fatal("OPEN_AI_API_KEY must be set")
    }
    if elevenLabsApiKey == "" {
        log.Fatal("ELEVEN_LABS_API_KEY must be set")
    }

    // Init Twilio client
    client := twilio.NewRestClientWithParams(twilio.ClientParams{
        Username: accountSid,
        Password: authToken,
    })

    // Fiber app
    app := fiber.New()

    // POST /call — kicks off outbound call & points TwiML at /twiml
    app.Post("/call", func(c *fiber.Ctx) error {
        var req callRequest
        if err := c.BodyParser(&req); err != nil {
            return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON"})
        }
        if req.To == "" {
            return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "`to` field is required"})
        }

        params := &openapi.CreateCallParams{}
        params.SetTo(req.To)
        params.SetFrom(fromNumber)
        params.SetUrl(fmt.Sprintf("%stwiml", baseUrl))
        params.SetMethod("GET")

        resp, err := client.Api.CreateCall(params)
        if err != nil {
            log.Printf("Twilio error: %v", err)
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to create call"})
        }

        return c.JSON(callResponse{SID: *resp.Sid, Message: "call initiated"})
    })

    // GET /twiml — returns the TwiML instructing Twilio to stream to /stream
    app.Get("/twiml", func(c *fiber.Ctx) error {
        callSid := c.Query("CallSid", "")
        if callSid == "" {
            return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "CallSid missing"})
        }

        xml := fmt.Sprintf(`
<Response>
  <Connect>
    <Stream url="%sstream?CallSid=%s" bidirectional="true"/>
  </Connect>
</Response>`, baseWsUrl, callSid)

        c.Type("xml")
        return c.SendString(xml)
    })

    // Middleware to require WebSocket upgrade on /stream
    app.Use("/stream", func(c *fiber.Ctx) error {
        if websocket.IsWebSocketUpgrade(c) {
            c.Locals("allowed", true)
            return c.Next()
        }
        return fiber.ErrUpgradeRequired
    })

    // WebSocket handler for Twilio media
    app.Get("/stream", websocket.New(func(ws *websocket.Conn) {
        defer ws.Close()
        log.Println("WebSocket /stream connected")
		// Dial into Deepgram
        dgURL := "wss://api.deepgram.com/v1/listen?model=nova-2-phonecall&encoding=mulaw&sample_rate=8000&channels=1&language=en-US&punctuate=true&smart_format=true&vad_events=true"
        header := http.Header{
            "Authorization": {fmt.Sprintf("Token %s", deepgramApiKey)},
        }
        dgConn, _, err := gws.DefaultDialer.Dial(dgURL, header)
        if err != nil {
            log.Printf("❌ Deepgram dial error: %v", err)
            return
        }
        defer dgConn.Close()

        openaiClient := openai.NewClient(openaiApiKey)

        openAiChannel := make(chan string)
		// Read Deepgram transcripts
        go func() {
            for {
                _, msg, err := dgConn.ReadMessage()
                if err != nil {
                    log.Printf("❌ Deepgram read error: %v", err)
                    return
                }
                handleDeepgramMessage(msg, openAiChannel)
            }
        }()
        
        elevenLabsChannel := make(chan string)
        go func() {
            for {
                text := <-openAiChannel

                go processText(openaiClient, text, elevenLabsChannel)
            }
        }()

        go func(){
            for {

                // process bot text, generate audio and send back to twilio.

                text := <- elevenLabsChannel
                log.Printf("Bot Text Recieved : %s", text);
                go streamElevenTTS(elevenLabsApiKey, ws, text, streamSid)
            }
        }()

        for {
            _, msg, err := ws.ReadMessage()
            if err != nil {
                log.Println("read error:", err)
                break
            }

            var ev twilioEvent
            if err := json.Unmarshal(msg, &ev); err != nil {
                log.Println("json unmarshal error:", err)
                continue
            }
            log.Printf("StreamSid: %s", streamSid)
            switch ev.Event {
            case "start":
                log.Printf("Stream started: CallSid=%s, StreamSid=%s\n",
                ev.Start.CallSid, ev.Start.StreamSid)
                streamSid = ev.Start.StreamSid

            case "media":
                // decode Base64 payload
                pcm, err := base64.StdEncoding.DecodeString(ev.Media.Payload)
                if err != nil {
                    log.Println("base64 decode error:", err)
                    continue
                }
                // send raw audio to Deepgram
                if err := dgConn.WriteMessage(gws.BinaryMessage, pcm); err != nil {
                    log.Printf("❌ Deepgram write error: %v", err)
                } else {
                    // log.Printf("📤 forwarded %d bytes to Deepgram", len(pcm))
                }

            case "stop":
                log.Println("Stream stopped")
                return

            default:
                log.Printf("unknown event: %s\n", ev.Event)
            }
        }
    }))

    // Start server
    addr := ":3000"
    fmt.Printf("Fiber server listening on %s\n", addr)
    log.Fatal(app.Listen(addr))
}
