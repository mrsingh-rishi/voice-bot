package tts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type ElevenLabsClient struct {
	APIKey string
	VoiceId string
	ModelId string
	OutputDeviceChannel chan <- string
}

func NewElevenLabsClient(apiKey string, voiceId string, modelId string, outputDeviceChannel chan <- string) (*ElevenLabsClient, error) {

	return &ElevenLabsClient{
		APIKey: apiKey,
		VoiceId: voiceId,
		ModelId: modelId,
		OutputDeviceChannel: outputDeviceChannel,
	}, nil
}

func (client *ElevenLabsClient) GenerateSpeech(text string) (error) {
	
	base, _ := url.Parse(
        fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s/stream/with-timestamps", client.VoiceId),
    )

	q := base.Query()
    q.Set("output_format", "ulaw_8000")
	base.RawQuery = q.Encode()
	// 2️⃣ Prepare JSON payload
	payload := map[string]interface{}{
		"text":     text,
		"model_id": "eleven_multilingual_v2",
		"voice_settings": map[string]float64{
			"stability":        0.75,
			"similarity_boost": 0.7,
		},
	}
	bodyBytes, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("❌ marshal payload: %w", err)
    }

	req, err := http.NewRequest("POST", base.String(), bytes.NewReader(bodyBytes))
    if err != nil {
        return fmt.Errorf("❌ build request: %w", err)
    }
    req.Header.Set("xi-api-key", client.APIKey)    // :contentReference[oaicite:8]{index=8}
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

	// 5️⃣ Read chunked audio and emit as media events
    dec := json.NewDecoder(resp.Body)

	for {
        // Define a struct matching exactly what you need
        var chunk struct {
            AudioBase64 string `json:"audio_base64"`
        }

        // Try to decode the next JSON object
        if err := dec.Decode(&chunk); err != nil {
            if err == io.EOF {
                // no more JSON objects
                break
            }
            return fmt.Errorf("failed to decode JSON chunk: %w", err)
        }

        // 3️⃣ Use the extracted audio_base64
        audioBase64 := chunk.AudioBase64

		// Send the audioBase64 to the output device channel
		client.OutputDeviceChannel <- audioBase64
    }

	return nil
}