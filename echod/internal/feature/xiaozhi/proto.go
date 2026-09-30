package xiaozhi

// The xiaozhi protocol, as far as M1 goes: a WebSocket carrying JSON text frames in both directions
// and, once there is audio, Opus in binary ones.
//
// There are about eight message types and every one of them is a `type` plus a few flat fields, so
// they are declared as one struct each rather than a tagged union. The one thing that is not
// optional is decoding: a message this file has never heard of is a message the session logs and
// carries on from, because the server is free to send one and a client that treats that as a fault
// drops a session for being spoken to.

// The message types this client sends or is sent.
const (
	TypeHello  = "hello"
	TypeListen = "listen"
	TypeAbort  = "abort"
	TypeSTT    = "stt"
	TypeLLM    = "llm"
	TypeTTS    = "tts"
	TypePing   = "ping"
	TypePong   = "pong"
)

// The states a listen moves through, and the reasons an abort gives.
const (
	ListenStart = "start"
	ListenStop  = "stop"

	// ListenAuto leaves the end of an utterance to the server's own voice activity detection. Any
	// other mode ends the turn on this device and sends an explicit stop, and the two are not
	// interchangeable: an endpoint of our own arguing with the server's about where the sentence
	// was would end turns it is still listening for.
	ListenAuto = "auto"

	// ListenManual is that other mode by name, for the turn this device opens on a wake word. The
	// protocol treats every non-auto mode the same way, so this is a word for us rather than one it
	// reads, and it exists so the call site says which of the two it meant.
	ListenManual = "manual"

	AbortWakeWord = "wake_word_detected"

	// AbortButton is somebody reaching for the action button to make the device stop talking. It is
	// free text the server logs and nothing else, and it is not the same as AbortWakeWord: the wake
	// word arrives with a question behind it, and this arrives with the opposite intent.
	AbortButton = "action_button"
)

// The states a tts moves through, which happen to be the same two words a listen uses and are not
// the same thing. A tts start is where the server says a sentence begins and a tts stop where it
// says it has sent the last of it — and neither is where that sentence begins or ends, because the
// packets in between take a cushion's worth of time to reach the card. The state of the sound and
// the state of the message are two different things with two different clocks.
const (
	TTSStart = "start"
	TTSStop  = "stop"

	// TTSSentenceStart carries the text of the sentence about to be spoken.
	TTSSentenceStart = "sentence_start"
)

// AudioParams is what a side says about the codec it is using.
//
// The two rates are different and neither is negotiable: the client sends 16 kHz, the capture rate
// and the rate it hears in, and the server answers 24 kHz, the rate it will send TTS in. The decoder
// has to be built from the number in the server's hello rather than the one in ours, and hardcoding
// a single rate for both is the most likely way to get a chipmunk.
type AudioParams struct {
	Format        string `json:"format,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	FrameDuration int    `json:"frame_duration,omitempty"`
}

// Features is what the client claims it can do, and on this device it claims nothing.
//
// Neither extension is advertised. Server-side echo cancellation is not something the official cloud
// offers to hand audio to, and this device cancels locally against the playback loopback, which is
// the part a server cannot do anyway. MCP is the other way round — its extension mechanism is tools
// the cloud calls into the device — so advertising it would be opening a remote-control surface in
// somebody's house for nothing.
type Features struct {
	MCP bool `json:"mcp,omitempty"`
	AEC bool `json:"aec,omitempty"`
}

// Hello is the first message each way. The client's carries what it can do; the server's carries
// the session id every later message has to quote and the rate it will send audio in.
type Hello struct {
	Type        string       `json:"type"`
	Version     int          `json:"version,omitempty"`
	Transport   string       `json:"transport,omitempty"`
	SessionID   string       `json:"session_id,omitempty"`
	Features    Features     `json:"features,omitempty"`
	AudioParams *AudioParams `json:"audio_params,omitempty"`
}

// Listen opens and closes the microphone. Mode is "auto" to leave the end of an utterance to the
// server's own voice activity detection, or anything else to send an explicit stop.
type Listen struct {
	Type      string `json:"type"`
	State     string `json:"state"`
	SessionID string `json:"session_id,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

// Abort cuts the turn short. Reason is free text the server logs; wake_word_detected is the one
// worth sending, because it is the difference between a barge-in and a client that fell over.
type Abort struct {
	Type      string `json:"type"`
	Reason    string `json:"reason,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// Event is any message the server sends, parsed loosely.
//
// The shaped messages are decoded into their own types where they matter, and this is what the rest
// of them — stt, llm, tts, and whatever arrives next — is read through. Text, state and the session
// id are the three fields the protocol repeats across all of them.
type Event struct {
	Type      string `json:"type"`
	State     string `json:"state,omitempty"`
	Text      string `json:"text,omitempty"`
	SessionID string `json:"session_id,omitempty"`

	// Bytes is how long a binary frame was, and Audio is what was in it. Before M3 the length was
	// all a downlink packet said, because there was no decoder to hand the rest of it to.
	Bytes int `json:"-"`

	// Audio is one Opus packet, and only on the events the server sent as a binary frame: every
	// other event has a nil one.
	//
	// It belongs to the callback rather than to the session that read it, and it is only good until
	// that callback returns — the read loop hands each packet straight to the decoder, which is the
	// one goroutine allowed to feed it, so nothing ever holds a packet after it has been decoded.
	Audio []byte `json:"-"`
}
