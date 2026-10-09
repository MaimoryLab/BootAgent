// Package modality decides which multimodal capabilities a Provider's models
// offer, and names the adapter that reaches each one. It sends no request
// itself: listing models and probing are the caller's, and a probe is billed,
// so only a user's click may send one.
package modality

// Capability IDs. They double as the suffix of the generated Skill ID
// (bootagent-<capability>) and as keys in the capability cache, so they must
// never be renamed.
const (
	Vision             = "vision"
	ImageGeneration    = "image-generation"
	SpeechToText       = "speech-to-text"
	TextToSpeech       = "text-to-speech"
	VideoGeneration    = "video-generation"
	VideoUnderstanding = "video-understanding"
)

// Capabilities lists every capability in the order the UI shows them.
var Capabilities = []string{Vision, ImageGeneration, SpeechToText, TextToSpeech, VideoGeneration, VideoUnderstanding}

// Adapter IDs: the calling convention the generated script uses, shared with
// the probes so a probe tests exactly what the Skill will send.
const (
	// AdapterChatVision sends images as Chat Completions image_url parts.
	AdapterChatVision = "openai-chat-vision"
	// AdapterChatVideo sends video as Chat Completions content parts, for models
	// that take video natively.
	AdapterChatVideo = "openai-chat-video"
	// AdapterChatImageOutput asks a chat model for an image in its reply
	// (gemini-*-image, gpt-image-* on JieKou.AI). How gateways return the image
	// is not yet verified, so models on this route install only after a probe.
	AdapterChatImageOutput = "openai-chat-image-output"
	// AdapterChatAudioInput sends audio as Chat Completions input_audio parts
	// (*-omni-*, gemini-*, mimo-v2.6-*). Unverified across gateways, like the
	// image-output route, so it installs only after a probe.
	AdapterChatAudioInput = "openai-chat-audio-input"
	// The OpenAI-compatible generation and audio routes.
	AdapterImages              = "openai-images"
	AdapterAudioTranscriptions = "openai-audio-transcriptions"
	AdapterAudioSpeech         = "openai-audio-speech"
	// Native routes at the Provider's host root, named by the catalog. Novita,
	// PPIO and JieKou.AI document the same native API family, so the adapters
	// are named after Novita, where it was first documented, and serve all
	// three: novita-async submits to /v3/async/<model> and polls
	// /v3/async/task-result; novita-audio posts to /v3/<model> and gets the
	// audio back in the response.
	AdapterNovitaAsync = "novita-async"
	AdapterNovitaAudio = "novita-audio"
	// AdapterPPIOVideo is reserved. PPIO's unified video route
	// (/v3/video/create) was documented when this design began and is not any
	// more; no catalog entry may name it until it can be verified again.
	AdapterPPIOVideo = "ppio-video"
)

// nativeAdapters reach the Provider's host root rather than its OpenAI base,
// so a Skill using one needs the catalog's native_base_url.
var nativeAdapters = map[string]bool{AdapterNovitaAsync: true, AdapterNovitaAudio: true, AdapterPPIOVideo: true}

// IsNative reports whether an adapter needs the Provider's native base URL.
func IsNative(adapter string) bool {
	return nativeAdapters[adapter]
}

// Generation options are the request fields beyond the prompt that an async
// generation model may document. The vocabulary is closed: the generated
// script maps each to one command-line flag and refuses anything else.
const (
	OptionDuration       = "duration"
	OptionSize           = "size"
	OptionAspectRatio    = "aspect_ratio"
	OptionNegativePrompt = "negative_prompt"
	OptionSound          = "sound"
)

// Options is the closed option vocabulary.
var Options = []string{OptionDuration, OptionSize, OptionAspectRatio, OptionNegativePrompt, OptionSound}

// Adapters is the closed vocabulary the catalog may name.
var Adapters = []string{
	AdapterChatVision, AdapterChatVideo, AdapterChatImageOutput, AdapterChatAudioInput,
	AdapterImages, AdapterAudioTranscriptions, AdapterAudioSpeech,
	AdapterNovitaAsync, AdapterNovitaAudio, AdapterPPIOVideo,
}

// Sources say how a capability was established, in resolution order.
const (
	SourceProbe    = "probe"
	SourceDeclared = "declared"
	SourceCatalog  = "catalog"
	SourceIDRule   = "id-rule"
)

// chatAdapter is the adapter a capability found on a chat model uses. Text to
// speech and video generation have no chat route: no rule claims them, and a
// chat model that declares audio or video output is not offered for either,
// because how a gateway returns that output is unknown.
var chatAdapter = map[string]string{
	Vision:             AdapterChatVision,
	VideoUnderstanding: AdapterChatVideo,
	SpeechToText:       AdapterChatAudioInput,
	ImageGeneration:    AdapterChatImageOutput,
}

// probeOnly are the adapters whose request and response format across gateways
// is not verified: neither a declaration nor a rule is enough to install them,
// only a probe that passed.
var probeOnly = map[string]bool{
	AdapterChatImageOutput: true,
	AdapterChatAudioInput:  true,
}

// Probe kinds tell the UI whether a capability can be checked live and whether
// the check needs a confirmation first.
const (
	// ProbeNone: no live check exists for this adapter yet, or ever (video
	// generation is a full billed generation that cannot be cancelled).
	ProbeNone = ""
	// ProbeMinimal: a request costing a token or so; the button warns of cost.
	ProbeMinimal = "minimal"
	// ProbeConfirm: a request that produces billed output, such as one image;
	// the UI asks before sending.
	ProbeConfirm = "confirm"
)

// probeKind lists the adapters that can be probed today. Others are added as
// their probes are written; until then the UI offers no button for them.
var probeKind = map[string]string{
	AdapterChatVision:          ProbeMinimal,
	AdapterAudioTranscriptions: ProbeMinimal,
	AdapterAudioSpeech:         ProbeMinimal,
	AdapterImages:              ProbeConfirm,
	AdapterNovitaAudio:         ProbeMinimal,
}

// ProbeKind reports how an adapter can be probed.
func ProbeKind(adapter string) string {
	return probeKind[adapter]
}

// adapterCapability is the capability each adapter serves. A probe or an
// install naming an adapter for another capability is refused.
var adapterCapability = map[string]string{
	AdapterChatVision:          Vision,
	AdapterChatVideo:           VideoUnderstanding,
	AdapterChatImageOutput:     ImageGeneration,
	AdapterChatAudioInput:      SpeechToText,
	AdapterImages:              ImageGeneration,
	AdapterAudioTranscriptions: SpeechToText,
	AdapterAudioSpeech:         TextToSpeech,
	AdapterNovitaAsync:         "",
	AdapterNovitaAudio:         TextToSpeech,
	AdapterPPIOVideo:           VideoGeneration,
}

// Serves reports whether an adapter can deliver a capability. The native
// adapters serve several, so for them the catalog entry decides.
func Serves(adapter, capability string) bool {
	served, known := adapterCapability[adapter]
	return known && (served == "" || served == capability)
}

// manualAdapter is the route a model the user names by hand is reached on.
// /models does not list dedicated image, transcription or speech models, so a
// user who knows one (gpt-image-1, whisper-1, tts-1) types its ID; it then
// installs only after a probe on this route passes.
var manualAdapter = map[string]string{
	Vision:          AdapterChatVision,
	ImageGeneration: AdapterImages,
	SpeechToText:    AdapterAudioTranscriptions,
	TextToSpeech:    AdapterAudioSpeech,
}

// ManualAdapter is the adapter for a hand-entered model, empty when the
// capability cannot be reached that way.
func ManualAdapter(capability string) string {
	return manualAdapter[capability]
}
