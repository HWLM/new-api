package dto

import relaydto "github.com/QuantumNous/new-api/relaykit/dto"

const (
	VideoStatusUnknown    = relaydto.VideoStatusUnknown
	VideoStatusQueued     = relaydto.VideoStatusQueued
	VideoStatusInProgress = relaydto.VideoStatusInProgress
	VideoStatusCompleted  = relaydto.VideoStatusCompleted
	VideoStatusFailed     = relaydto.VideoStatusFailed
)

type OpenAIVideo = relaydto.OpenAIVideo
type OpenAIVideoError = relaydto.OpenAIVideoError

var NewOpenAIVideo = relaydto.NewOpenAIVideo
