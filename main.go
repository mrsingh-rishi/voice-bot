package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/joho/godotenv"
	"github.com/mrsingh-rishi/voice-bot/call"
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

// Twilio's streaming payload
type twilioEvent struct {
	Event string `json:"event"` // "start", "media", "stop"
	Media struct {
		Payload string `json:"payload"` // base64 audio
	} `json:"media"`
	Start struct {
		// Normally contains CallSid, streamSid, etc.
		CallSid   string `json:"callSid"`
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
	authToken := os.Getenv("TWILIO_AUTH_TOKEN")
	fromNumber := os.Getenv("TWILIO_FROM_NUMBER")
	baseUrl := os.Getenv("BASE_URL")
	baseWsUrl := os.Getenv("BASE_WS_URL")
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
		if !websocket.IsWebSocketUpgrade(c) {
			return fiber.ErrUpgradeRequired
		}
		c.Locals("allowed", true)
		return c.Next()
	})

	// WebSocket handler for Twilio media
	app.Get("/stream", websocket.New(func(ws *websocket.Conn) {
		// Ensure the connection is properly upgraded
		if ws.Conn == nil {
			log.Println("WebSocket connection not properly upgraded")
			return
		}

		log.Println("WebSocket connection established")

		call, err := call.NewCall(ws)
		if err != nil {
			log.Printf("Error creating call: %v", err)
			return
		}
        // **Block** here — Start() will itself read from `ws` in StartReceivingAudio
        defer call.CleanupResources()
        call.Start()
	}))

	// Start server
	addr := ":3000"
	fmt.Printf("Fiber server listening on %s\n", addr)
	log.Fatal(app.Listen(addr))
}
