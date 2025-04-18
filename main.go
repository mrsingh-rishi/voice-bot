package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	gws "github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	twilio "github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
)

type callRequest struct {
    To string `json:"to"`
}

type callResponse struct {
    SID     string `json:"sid,omitempty"`
    Message string `json:"message"`
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

		// Read Deepgram transcripts
        go func() {
            for {
                _, msg, err := dgConn.ReadMessage()
                if err != nil {
                    log.Printf("❌ Deepgram read error: %v", err)
                    return
                }
                log.Printf("📝 Deepgram transcript: %s", string(msg))
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

            switch ev.Event {
            case "start":
                log.Printf("Stream started: CallSid=%s, StreamSid=%s\n",
                    ev.Start.CallSid, ev.Start.StreamSid)

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
                    log.Printf("📤 forwarded %d bytes to Deepgram", len(pcm))
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
