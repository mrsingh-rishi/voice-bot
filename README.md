# voice-bot

A Twilio-based conversational voice bot that performs real-time speech-to-text (STT), conversational LLM responses, and text-to-speech (TTS) to stream audio back to the caller.

## High-level architecture

The app is structured into small components that handle specific responsibilities. Data flows mostly through channels between workers so pieces remain decoupled and testable.

## Architecture versions (change log)

We track the high-level architecture evolution here so each architectural change is documented with a version, date, a short summary, and notes about files impacted. Update this section whenever the architecture changes.

- v0.1 — 2024-01-10 — initial PoC wiring
  - Summary: Minimal Twilio WebSocket receiver, basic STT -> LLM -> TTS flow.
  - Files: `main.go`, `call/callsession.go`, basic `stt/` and `output/` wiring.
  - Notes: Simple end-to-end proof-of-concept. No robust worker abstraction or cancellation.

- v0.2 — 2024-06-22 — worker refactor
  - Summary: Introduced `workers/` package and split responsibilities into Agent, AgentResponse and filler workers. Streaming pipeline converted to channels between workers.
  - Files: `workers/*`, `llm/openai.go` (streaming helper), `call/callsession.go` updated.
  - Notes: Allowed easier testing and clearer boundaries between STT, LLM and TTS.

- v0.3 — 2024-11-05 — Deepgram integration
  - Summary: Added `stt/deepgram.go` to stream audio to Deepgram and receive final transcriptions. Deepgram client supports broadcasting to multiple consumers.
  - Files: `stt/deepgram.go`, `call/callsession.go` (connected Deepgram client), channels added for transcription distribution.
  - Notes: Final transcriptions are broadcast to both main processing and filler-response generator.

- v0.4 — 2025-03-14 — TTS streaming and chunking
  - Summary: TTS client (`tts/elevenlabs.go`) updated to stream chunked audio and use a sentinel `EndOfUtterance`. Output layer (`output/twilio.go`) emits Twilio `media` and `mark` events.
  - Files: `tts/elevenlabs.go`, `output/twilio.go`
  - Notes: Chunked audio improved latency; introduced clear sentinel patterns for better flow control.

- v0.5 — 2025-10-07 — Interruption handling (current)
  - Summary: Added a dedicated transcription-control channel and a `TranscriptionWorker` that decides whether an incoming final transcription should interrupt current processing. Workers become cancelable and expose `IsBusy()` so the transcription worker can detect mid-response interruptions. On interruption the worker sends a `__CLEAR__` sentinel, stops & recreates the Agent and AgentResponse workers, and forwards the interrupting transcription for immediate processing.
  - Files: `call/transcriptionworker.go`, `call/callsession.go` (wiring), `stt/deepgram.go` (third channel), `workers/agentworker.go`, `workers/agentresponseworker.go` (busy flags and ctx cancellation), `llm/openai.go` (StreamResponseWithContext), `tts/elevenlabs.go` (context-aware GenerateSpeech), `output/twilio.go` (sentinel handling basics)
  - Notes: This release prioritizes responsiveness for live callers. Worker recreation currently resets LLM conversation history; consider preserving history when needed.

How to add a new architecture version entry
- Update this section with a new `vX.Y` entry, the date, a short summary, the primary files changed, and any migration or operational notes. Link to PRs or issue numbers if your project tracks them.


Components
- main.go — HTTP server and WebSocket/TwiML wiring. Creates `Call` objects when Twilio opens a WebSocket stream.
- call/ — Call session management and orchestration.
  - `callsession.go` — `Call` struct that wires workers and channels together, receives Twilio events, starts/stops workers and the Deepgram audio pipeline.
  - `transcriptionworker.go` — (new) listens for final transcriptions on a control channel, decides if a transcription should interrupt current processing, and handles clearing/restarting the agent pipeline.
- stt/ — Speech-to-text
  - `deepgram.go` — Deepgram WebSocket client that sends audio and receives final transcriptions. It now broadcasts final transcriptions to multiple channels (main transcription, filler-response input, and the transcription-control channel for interruption detection).
- workers/ — Worker primitives that do the heavy lifting
  - `agentworker.go` — Sends final transcriptions to the LLM (OpenAI client) and streams sentences to the `StreamingChannel`.
  - `agentresponseworker.go` — Consumes streaming sentences, sends them to TTS, and writes chunked audio to the `OutputChannel`.
  - `fillerresponseworker.go` — Runs a small helper model used for filler-word predictions (unchanged).
- llm/ — OpenAI client wrapper (streams responses). The OpenAI streaming method now supports cancellation via context so the agent worker can be stopped mid-stream.
- tts/ — ElevenLabs TTS client. GenerateSpeech accepts a context so in-flight TTS HTTP requests can be canceled on interruption.
- output/ — Twilio output wiring
  - `twilio.go` — Sends media/mark events to the Twilio WebSocket. The output channel receives base64 audio chunks. It also recognizes an `EndOfUtterance` sentinel. The transcription worker now sends a `__CLEAR__` sentinel to notify the output layer of interruptions (see notes).

### Architecture diagram

Below is an ASCII diagram that shows the main runtime components, channels, and the flow of data. This should help visualize how audio and transcriptions flow through the system.

```
                        +----------------+
                        |   Twilio RTC   |  <-- WebSocket connection (media events)
                        +--------+-------+
                                 |
                                 | media (base64)
                                 v
                             Call.StartReceivingAudio
                                 |
                                 | decoded frames (bytes)
                                 v
                            AudioChannel ---+
                                            |
                                            v
                                      Deepgram Client
                                      (stt/deepgram.go)
                                            |
            +-------------------------------+-------------------------------+
            |                                                               |
            v                                                               v
  TranscriptionChannel                                             TranscriptionControlChannel
  (AgentWorker input)                                                 (TranscriptionWorker)
            |                                                               |
            v                                                               v
      AgentWorker ------> StreamingChannel -----> AgentResponseWorker ----> OutputChannel ---> TwilioOutput
   (llm/openai.go)   (sentences/chunks)        (tts/elevenlabs.go)       (audio chunks/base64)
            |
            v
     (optionally store or use OpenAI messages)

  FillerResponseWorker <- TranscriptionChannel (duplicate) (used for filler heuristics)

  TranscriptionWorker monitors TranscriptionControlChannel and triggers an interruption
  flow (clear sentinel -> stop workers -> recreate workers -> forward transcription)

```

### Component responsibilities (detailed)

- main.go
  - Accepts HTTP calls to create Twilio calls.
  - Serves TwiML that instructs Twilio to stream audio to `/stream`.
  - Upstreams new WebSocket connections to `Call` instances.

- call/callsession.go (Call)
  - Orchestrates the lifetime of a single Twilio stream/call.
  - Creates the channels, workers, and the Deepgram client.
  - Coordinates start/stop, output worker creation and signal channels.
  - Stores API keys and voice/model identifiers so workers can be recreated on interruptions.

- call/transcriptionworker.go
  - Receives a duplicate stream of final transcriptions on `TranscriptionControlChannel`.
  - Decides whether the incoming transcription should interrupt existing work by querying `AgentWorker.IsBusy()` and `AgentResponseWorker.IsBusy()`.
  - On interrupt: sends a `__CLEAR__` sentinel to `OutputChannel`, stops current agent workers, recreates and restarts them, then forwards the interrupting transcription to `TranscriptionChannel`.

- stt/deepgram.go
  - Maintains a WebSocket to Deepgram and writes raw audio frames to it.
  - Parses Deepgram JSON responses and broadcasts final transcriptions to multiple channels: main `TranscriptionChannel`, `TranscriptionChannel2` (filler), and `TranscriptionControlChannel`.

- workers/agentworker.go
  - Listens on `TranscriptionChannel` for final transcriptions and calls the `llm` client.
  - Uses `OpenAIClient.StreamResponseWithContext(ctx, text)` so streaming can be canceled when `Stop()` is invoked.
  - Emits sentence-level results to `StreamingChannel` for more natural TTS chunking.
  - Exposes `IsBusy()` to indicate when the worker is processing a transcription.

- workers/agentresponseworker.go
  - Listens on `StreamingChannel` for complete sentences or fragments.
  - Calls `tts.ElevenLabsClient.GenerateSpeech(ctx, sentence)` which streams chunked audio to `OutputChannel`.
  - Uses a `busy` flag and context cancellation so the transcription worker can detect when speech is ongoing and cancel in-flight TTS.

- workers/fillerresponseworker.go
  - Accepts duplicate transcriptions and uses a small OpenAI prompt to choose a filler word.
  - Designed for heuristics/UI but kept separate to avoid blocking main processing.

- llm/openai.go
  - Wraps the OpenAI streaming API and converts incoming deltas into full sentences using a regex buffer.
  - Provides `StreamResponseWithContext` so callers can cancel long-running streams.

- tts/elevenlabs.go
  - Calls ElevenLabs TTS streaming API and decodes chunked JSON responses containing base64 audio.
  - Emits base64 audio chunks to `OutputChannel` followed by `EndOfUtterance` sentinel to mark completion.

- output/twilio.go
  - Listens on `OutputChannel` for base64 chunks or sentinels.
  - Sends Twilio `media` events for audio chunks and `mark` events for `EndOfUtterance`.
  - Can be extended to listen for `__CLEAR__` to explicitly stop/flush the current utterance.


## Channels & Data Flow

- WebSocket (Twilio) -> `Call.StartRecievingAudio()` -> `AudioChannel` -> Deepgram WebSocket write
- Deepgram -> receives transcripts and broadcasts final results to:
  - `TranscriptionChannel` (AgentWorker input)
  - `FillerResponseWorker` input
  - `TranscriptionControlChannel` (TranscriptionWorker input for interruption logic)
- `AgentWorker` -> `StreamingChannel` (sentence fragments) -> `AgentResponseWorker`
- `AgentResponseWorker` -> `OutputChannel` (base64 audio chunks & `EndOfUtterance` sentinel) -> `TwilioOutput` -> Twilio WebSocket

## Interruption handling (new)

- A new transcription worker (`call/transcriptionworker.go`) listens to `TranscriptionControlChannel` which receives a duplicate of final transcriptions from Deepgram.
- If a final transcription arrives while either `AgentWorker` or `AgentResponseWorker` is busy (they expose `IsBusy()`), the transcription worker treats it as an interruption.
- On interruption, the worker:
  - emits a `__CLEAR__` sentinel to the `OutputChannel` to indicate downstream output should be cleared (you may wire custom handling in the output layer to stop/flush any ongoing audio)
  - calls `.Stop()` on the agent/response workers (their contexts cancel; OpenAI/TTS calls are cancelable)
  - recreates fresh worker instances (preserving API keys stored on `Call`) and starts them
  - forwards the interruption transcription to the main `TranscriptionChannel` for immediate processing by the newly started agent worker

This approach allows live user speech to interrupt the bot speaking and get immediate attention.

## Current features

- Outbound call initiation (via `POST /call` in `main.go`) and TwiML that instructs Twilio to stream audio to the app.
- Twilio WebSocket handler (`/stream`) that accepts Twilio streaming events and routes audio into the Deepgram client.
- Real-time speech-to-text via Deepgram.
- Conversational responses via OpenAI (streamed sentence-by-sentence into the streaming pipeline).
- Text-to-speech via ElevenLabs; chunked audio is sent back over the Twilio WebSocket.
- Interruption handling: final transcriptions can interrupt in-progress LLM/TTS work (see above).

## Environment variables

The following environment variables must be set (examples are in `.env` in development):

- TWILIO_ACCOUNT_SID
- TWILIO_AUTH_TOKEN
- TWILIO_FROM_NUMBER
- BASE_URL (public URL that Twilio can reach, e.g., the ngrok URL or your public host)
- BASE_WS_URL (WebSocket base URL used in TwiML)
- DEEPGRAM_API_KEY
- OPEN_AI_API_KEY
- ELEVEN_LABS_API_KEY
- PORT (optional; defaults to 3000)

## How to run (development)

1. Set environment variables (or create a `.env`) with the values above.
2. Build & run:

```bash
cd /path/to/voice-bot
go build ./...
./voice-bot
```

3. Use `POST /call` to initiate an outbound call or connect Twilio to your `/twiml` endpoint for inbound calls.

## Notes & next steps

- Output handling for the `__CLEAR__` sentinel needs to be wired in `output/twilio.go` if you want to take explicit action on clear events beyond receiving them on the `OutputChannel` (for example, stopping any buffered sending or truncating outgoing audio). I added the sentinel so higher-level logic can be implemented without touching worker internals.
- The current worker recreation strategy resets the agent's conversation history because a fresh `OpenAIClient` is created when the agent worker is recreated. If you want interruptions to preserve conversation context, consider sharing or serializing `OpenAIClient.Messages` when recreating.
- Buffer sizes (channel sizes) may require tuning when under load to avoid dropped transcriptions or audio.
- Consider adding tests for transcription worker behavior (deciding interruptions, worker restart flow) and for graceful shutdown.

## Files of interest

- `main.go`
- `call/callsession.go`
- `call/transcriptionworker.go`
- `stt/deepgram.go`
- `llm/openai.go`
- `tts/elevenlabs.go`
- `workers/*`
- `output/twilio.go`

## Contact / Contribution

Open issues or submit PRs. If you want me to also wire `__CLEAR__` handling in `output/twilio.go`, add tests, or adjust the restart behavior to preserve conversation state, tell me which you'd like next and I'll implement it.
