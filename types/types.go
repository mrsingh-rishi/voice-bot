package types

import "context"

type TranscriptionChannel struct {
	Transcription string
	Confidence    float64
	Final         bool
}

type TranscriptionResult struct {
	Transcription string
	Confidence    float64
	Final         bool
	Ctx           context.Context
}