package realtime

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// openAI is OpenAI's Realtime API as released (its session.update is that version's), and xAI's, which
// speaks the same events. The audio and transcript events are read by their beta names too, which some
// servers still send; what is sent is the released form only.
type openAI struct {
	base         string
	defaultModel string
	defaultVoice string
	// transcribe is the model that writes down what was heard: OpenAI's own. xAI is sent none, and
	// writes it down by itself.
	transcribe string
}

const openAIRate = 24000

func (o openAI) address(c Config) (string, http.Header, error) {
	addr, keyOK, err := endpoint(c, o.base)
	if err != nil {
		return "", nil, err
	}
	u, err := url.Parse(addr)
	if err != nil {
		return "", nil, err
	}
	model := c.Model
	if model == "" {
		model = o.defaultModel
	}
	q := u.Query()
	q.Set("model", model)
	u.RawQuery = q.Encode()
	h := http.Header{}
	switch {
	case c.Key != "" && keyOK:
		h.Set("Authorization", "Bearer "+c.Key)
	case c.Endpoint == "" && c.Key == "":
		return "", nil, ErrNoKey
	}
	return u.String(), h, nil
}

func (o openAI) setup(c Config) []any {
	voice := c.Voice
	if voice == "" {
		voice = o.defaultVoice
	}
	tools := []map[string]any{}
	for _, f := range c.Functions {
		params := f.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{"type": "function", "name": f.Name, "description": f.Description, "parameters": params})
	}
	input := map[string]any{
		"format":         map[string]any{"type": "audio/pcm", "rate": openAIRate},
		"turn_detection": nil, // the device says when somebody has finished
	}
	if o.transcribe != "" {
		input["transcription"] = map[string]any{"model": o.transcribe}
	}
	return []any{map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type":         "realtime",
			"instructions": c.Instructions,
			"audio": map[string]any{
				"input": input,
				"output": map[string]any{
					"format": map[string]any{"type": "audio/pcm", "rate": openAIRate},
					"voice":  voice,
				},
			},
			"tools":       tools,
			"tool_choice": "auto",
		},
	}}
}

func (o openAI) audio(pcm16k []int16) any {
	return map[string]any{"type": "input_audio_buffer.append", "audio": pcmBase64(upTo24k(pcm16k))}
}

func (o openAI) endOfSpeech() []any {
	return []any{
		map[string]any{"type": "input_audio_buffer.commit"},
		map[string]any{"type": "response.create"},
	}
}

func (o openAI) toolResult(callID, name, output string) []any {
	return []any{map[string]any{"type": "conversation.item.create", "item": map[string]any{
		"type": "function_call_output", "call_id": callID, "output": output,
	}}}
}

// goOn asks for the next response: one, after all of a response's calls are answered, since a second
// while one is going is refused.
func (o openAI) goOn() []any { return []any{map[string]any{"type": "response.create"}} }

func (o openAI) ready([]byte) bool { return true }
func (o openAI) readyFirst() bool  { return true }

// nonFatal are errors that refuse one request and leave the session as it was.
var nonFatal = map[string]bool{
	"conversation_already_has_active_response": true,
	"response_cancel_not_active":               true,
}

func (o openAI) read(msg []byte, emit func(Event)) {
	var m struct {
		Type       string `json:"type"`
		Delta      string `json:"delta"`
		Transcript string `json:"transcript"`
		Item       struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"item"`
		Response struct {
			Status        string `json:"status"`
			StatusDetails struct {
				Reason string `json:"reason"`
				Error  struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"status_details"`
			Output []struct {
				Type string `json:"type"`
			} `json:"output"`
		} `json:"response"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(msg, &m) != nil {
		return
	}
	switch m.Type {
	case "response.output_audio.delta", "response.audio.delta":
		if pcm := pcmFrom(m.Delta); len(pcm) > 0 {
			emit(Event{Kind: Audio, PCM: pcm, Rate: openAIRate})
		}
	case "response.output_audio_transcript.delta", "response.audio_transcript.delta":
		emit(Event{Kind: Saying, Text: m.Delta})
	case "response.output_audio_transcript.done", "response.audio_transcript.done":
		emit(Event{Kind: Said, Text: m.Transcript})
	case "conversation.item.input_audio_transcription.completed":
		emit(Event{Kind: Heard, Text: m.Transcript})
	case "response.output_item.done":
		// A call is read from the item whole, which always has its name.
		if m.Item.Type == "function_call" {
			emit(Event{Kind: Call, CallID: m.Item.CallID, Name: m.Item.Name, Args: m.Item.Arguments})
		}
	case "response.done":
		// A response that called functions is not the answer: the answer comes after their results.
		calls := false
		for _, o := range m.Response.Output {
			if o.Type == "function_call" {
				calls = true
			}
		}
		switch m.Response.Status {
		case "cancelled":
			emit(Event{Kind: Interrupt})
		case "failed", "incomplete":
			why := m.Response.StatusDetails.Error.Message
			if why == "" {
				why = m.Response.StatusDetails.Reason
			}
			emit(Event{Kind: Failed, Err: errors.New("realtime: the answer " + m.Response.Status + ": " + why)})
		default:
			if calls {
				emit(Event{Kind: Calls})
			} else {
				emit(Event{Kind: Done})
			}
		}
	case "error":
		if m.Error.Code == "input_audio_buffer_commit_empty" {
			// Nothing was heard: said so, and the session is ended rather than left to answer silence.
			emit(Event{Kind: Empty})
			return
		}
		if nonFatal[m.Error.Code] {
			return
		}
		emit(Event{Kind: Failed, Err: errors.New("realtime: " + m.Error.Message)})
	}
}
