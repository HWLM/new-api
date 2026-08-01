package dto

import relaydto "github.com/QuantumNous/new-api/relaykit/dto"

type ChannelSettings = relaydto.ChannelSettings

const (
	HTTPProtocolAuto         = relaydto.HTTPProtocolAuto
	HTTPProtocolHTTP1        = relaydto.HTTPProtocolHTTP1
	MaxHTTP2ConnectionShards = relaydto.MaxHTTP2ConnectionShards
)

type VertexKeyType = relaydto.VertexKeyType

const (
	VertexKeyTypeJSON   = relaydto.VertexKeyTypeJSON
	VertexKeyTypeAPIKey = relaydto.VertexKeyTypeAPIKey
)

type AwsKeyType = relaydto.AwsKeyType

const (
	AwsKeyTypeAKSK   = relaydto.AwsKeyTypeAKSK
	AwsKeyTypeApiKey = relaydto.AwsKeyTypeApiKey
)

type ChannelOtherSettings = relaydto.ChannelOtherSettings
type VideoRequestFormat = relaydto.VideoRequestFormat

const (
	VideoRequestFormatOpenAI     = relaydto.VideoRequestFormatOpenAI
	VideoRequestFormatSeedanceV3 = relaydto.VideoRequestFormatSeedanceV3
)

type SeedanceV3Routes = relaydto.SeedanceV3Routes
type SeedanceV3Route = relaydto.SeedanceV3Route

var IsSeedanceV3RouteMethodAllowed = relaydto.IsSeedanceV3RouteMethodAllowed

type AdvancedCustomConfig = relaydto.AdvancedCustomConfig
type AdvancedCustomRoute = relaydto.AdvancedCustomRoute
type AdvancedCustomRouteAuth = relaydto.AdvancedCustomRouteAuth

const (
	AdvancedCustomAuthTypeNone   = relaydto.AdvancedCustomAuthTypeNone
	AdvancedCustomAuthTypeHeader = relaydto.AdvancedCustomAuthTypeHeader
	AdvancedCustomAuthTypeQuery  = relaydto.AdvancedCustomAuthTypeQuery
)

const (
	AdvancedCustomConverterNone                                         = "none"
	AdvancedCustomConverterAnthropicMessagesToOpenAIChatCompletions     = "anthropic_messages_to_openai_chat_completions"
	AdvancedCustomConverterOpenAIChatCompletionsToAnthropicMessages     = "openai_chat_completions_to_anthropic_messages"
	AdvancedCustomConverterOpenAIChatCompletionsToOpenAIResponses       = "openai_chat_completions_to_openai_responses"
	AdvancedCustomConverterOpenAIResponsesToOpenAIChatCompletions       = "openai_responses_to_openai_chat_completions"
	AdvancedCustomConverterGeminiGenerateContentToOpenAIChatCompletions = "gemini_generate_content_to_openai_chat_completions"
	AdvancedCustomConverterOpenAIChatCompletionsToGeminiGenerateContent = "openai_chat_completions_to_gemini_generate_content"
)
