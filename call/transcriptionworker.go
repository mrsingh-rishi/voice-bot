package call

import (
	"log"
	"time"

	"github.com/mrsingh-rishi/voice-bot/workers"
)

// ClearEvent is sent to output to indicate interruption/clear. The output
// layer can interpret this sentinel to stop or flush any buffered audio being
// sent to Twilio. It is intentionally a simple string so channels remain
// `chan string` for existing wiring.
const ClearEvent = "__CLEAR__"

// StartTranscriptionWorker listens for transcriptions on the control channel
// and decides whether the incoming transcription should interrupt current
// processing. This worker runs in its own goroutine and is safe to start once
// per Call.

// Decision rule: if the AgentWorker or the AgentResponseWorker is currently
// busy producing or speaking (they expose IsBusy()), the incoming final
// transcription is treated as an interruption.

// On interruption:
//   - send a ClearEvent to c.OutputChannel so output layer can react
//   - stop current AgentResponseWorker and AgentWorker (their contexts cancel)
//   - recreate the workers using stored API keys on Call
//   - start the new workers
//   - forward the incoming transcription to the main TranscriptionChannel
//     so the (new) AgentWorker can immediately process it
func (c *Call) StartTranscriptionWorker() {
	go func() {
		log.Println("Transcription worker started")
		for {
			select {
			case <-c.done:
				log.Println("Transcription worker done")
				return
			case t, ok := <-c.TranscriptionControlChannel:
				if !ok {
					log.Println("Transcription control channel closed")
					return
				}
				if t == "" {
					// ignore empty transcriptions
					continue
				}

				// Determine if incoming transcription should interrupt.
				// We consider both agent thinking (IsBusy on AgentWorker) and
				// agent speaking (IsBusy on AgentResponseWorker).
				isInterrupt := false
				if c.AgentWorker != nil && c.AgentWorker.IsBusy() {
					isInterrupt = true
				}
				if c.AgentResponseWorker != nil && c.AgentResponseWorker.IsBusy() {
					isInterrupt = true
				}

				if isInterrupt {
					log.Println("Interruption detected: clearing current response and restarting workers")

					// Notify output layer to clear any in-flight audio. Use
					// non-blocking send so we never hang the transcription
					// goroutine when output is temporarily busy.
					select {
					case c.OutputChannel <- ClearEvent:
					default:
					}

					// Stop currently active workers. Their internal contexts
					// should cancel in-flight OpenAI/TTS work (we wired
					// streaming calls to use the workers' contexts).
					if c.AgentResponseWorker != nil {
						c.AgentResponseWorker.Stop()
					}
					if c.AgentWorker != nil {
						c.AgentWorker.Stop()
					}

					// Small pause to let goroutines unwind. This is pragmatic
					// and can be replaced with explicit synchronization if
					// desired.
					time.Sleep(150 * time.Millisecond)

					// Recreate agent worker and response worker so they start
					// fresh. We use the stored API keys and voice identifiers on
					// the Call struct for recreation.
					aw, err := workers.NewAgentWorker(c.OpenAIApiKey, "gpt-4o-mini", c.StreamingChannel, c.TranscriptionChannel)
					if err != nil {
						log.Printf("Failed to recreate AgentWorker after interruption: %v", err)
						// If recreation fails, skip restarting for now.
						continue
					}
					rw, err2 := workers.NewAgentResponseWorker(c.ElevenLabsApiKey, c.ElevenVoiceId, c.ElevenModelId, c.StreamingChannel, c.OutputChannel)
					if err2 != nil {
						log.Printf("Failed to recreate AgentResponseWorker after interruption: %v", err2)
						continue
					}

					c.AgentWorker = aw
					c.AgentResponseWorker = rw
					c.AgentWorker.Start()
					c.AgentResponseWorker.Start()
					log.Println("Workers restarted after interruption")
				}

				// Forward the transcription to the main transcription channel
				// for processing by the (new) AgentWorker. Try a non-blocking
				// send first, then retry briefly; avoid blocking forever.
				select {
				case c.TranscriptionChannel <- t:
				default:
					select {
					case c.TranscriptionChannel <- t:
					case <-time.After(100 * time.Millisecond):
						log.Println("Dropping transcription due to busy channel")
					}
				}
			}
		}
	}()
}
