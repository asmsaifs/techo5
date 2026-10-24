package xiaozhi

import "testing"

func TestTranscriptDoesNotAdvertiseAnOpenMicrophone(t *testing.T) {
	var states []State
	cancel := Changed.Listen(func(s State) { states = append(states, s) })
	defer cancel()
	startScreen()
	finishScreen(false)
	onScreenEvent(Event{Type: TypeSTT, Text: "Marvin"})
	if got := states[len(states)-1]; got.Phase != "thinking" || got.Heard != "Marvin" {
		t.Fatalf("closed microphone transcript advertised %+v; want thinking", got)
	}
}

func TestScreenTracksCaptureAndDoesNotInventFollowUp(t *testing.T) {
	var latest State
	cancel := Changed.Listen(func(s State) { latest = s })
	defer cancel()
	startScreen()
	if latest.Phase != "listening" {
		t.Fatalf("open capture: %+v", latest)
	}
	finishScreen(true)
	if latest.Phase != "thinking" {
		t.Fatalf("natural endpoint: %+v", latest)
	}
	onScreenEvent(Event{Type: TypeSTT, Text: "Hôm nay ngày mấy?"})
	if latest.Phase != "thinking" {
		t.Fatalf("transcript reopened capture: %+v", latest)
	}
	onScreenEvent(Event{Type: TypeTTS, State: TTSStart})
	finishScreen(true)
	if latest.Phase != "replying" {
		t.Fatalf("late close overwrote TTS: %+v", latest)
	}
	onScreenEvent(Event{Type: TypeLLM, Text: "🙂"})
	if latest.Phase != "replying" {
		t.Fatalf("late LLM overwrote TTS: %+v", latest)
	}
	onScreenEvent(Event{Type: TypeTTS, State: TTSStop})
	if latest.Phase != "idle" {
		t.Fatalf("TTS stop invented capture: %+v", latest)
	}
	startScreen()
	if latest.Phase != "listening" {
		t.Fatalf("actual follow-up did not open: %+v", latest)
	}
	finishScreen(false)
	if latest.Phase != "idle" {
		t.Fatalf("canceled capture: %+v", latest)
	}
}
