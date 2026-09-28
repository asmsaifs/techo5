//go:build linux

// xiaozhi-m0 is the milestone M0 throwaway from docs/xiaozhi-plan.md: it asks the
// official cloud whether a CELT-only Opus uplink is understood. It stands up the
// OTA + activation + hello + listen sequence, pushes N seconds of the real
// microphone as 60 ms CELT frames, and prints whatever the server says it heard.
//
// Exit criteria: a correct transcript of at least 90% of spoken words over 30
// seconds, twice, and end-to-end under 1.5 s from end-of-speech to the first TTS
// byte. The transcript check is done by whoever reads the output; the latency is
// printed here.
//
// Run as root on the Show with the daemon stopped:
//
//	killall techo5
//	./xiaozhi-m0 -seconds 30 -out /data/misc/techo5/m0.wav
package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tphakala/go-opus/opus"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/audio"
)

// celtFrameSamples is one 60 ms CELT frame at the 16 kHz capture rate.
const celtFrameSamples = mic.Rate * 60 / 1000

func main() {
	otaURL := flag.String("ota", "https://api.tenclass.net/xiaozhi/ota/", "OTA endpoint to ask for the WebSocket url and token")
	seconds := flag.Int("seconds", 30, "seconds of microphone audio to push")
	out := flag.String("out", "", "write the pushed audio as a 16 kHz mono WAV here")
	deviceID := flag.String("device-id", "", "MAC with colons; default reads layout.FactoryMAC()")
	clientID := flag.String("client-id", "", "UUID v4 with dashes; default generates one and prints it")
	lang := flag.String("lang", "zh-CN", "Accept-Language for the OTA call")
	mode := flag.String("mode", "manual", `listen mode: "manual" sends an explicit listen stop at the end ("auto" relies on server VAD)`)
	bitrate := flag.Int("bitrate", 24000, "uplink Opus bitrate, CBR")
	complexity := flag.Int("complexity", 4, "uplink Opus complexity, 1..10")
	activateFor := flag.Duration("activate-timeout", 10*time.Minute, "how long to poll the OTA endpoint waiting on the code to be redeemed")
	replyFor := flag.Duration("reply-timeout", 30*time.Second, "how long to wait after end-of-speech for the server's answer")
	flag.Parse()

	if *seconds <= 0 {
		fatal("want a positive -seconds")
	}

	dev := *deviceID
	if dev == "" {
		mac, err := layout.FactoryMAC()
		if err != nil {
			fatal("device-id: %v", err)
		}
		dev = mac
	}
	if !strings.Contains(dev, ":") {
		fatal("device-id must be a MAC with colons (the OTA endpoint rejects the uncolored form)")
	}

	cli := *clientID
	if cli == "" {
		cli = newUUIDv4()
		fmt.Printf("client-id: %s (reuse with -client-id to keep the same identity)\n", cli)
	}

	iota := time.Now()
	info := fetchOTA(*otaURL, dev, cli, *lang)
	if act, ok := info.activation(); ok {
		fmt.Printf("activation needed:\n    code: %s\n    message: %q\n", act.code, act.message)
		pollActivation(*otaURL, dev, cli, *lang, *activateFor)
	} else {
		fmt.Println("already activated")
	}
	fmt.Printf("ota took %v\n", time.Since(iota).Round(time.Millisecond))

	wsURL, token := info.ws()
	fmt.Printf("connecting %s\n", wsURL)
	conn, err := dial(wsURL, token, dev, cli)
	if err != nil {
		fatal("websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	session, srvRate, err := hello(conn)
	if err != nil {
		fatal("hello: %v", err)
	}
	fmt.Printf("session %s, server downlink %d Hz\n", session, srvRate)

	if err := writeJSON(conn, map[string]any{
		"session_id": session,
		"type":       "listen",
		"state":      "start",
		"mode":       *mode,
	}); err != nil {
		fatal("listen start: %v", err)
	}

	endOfSpeech := captureAndSend(conn, session, *seconds, *out, *bitrate, *complexity)

	if *mode == "manual" {
		if err := writeJSON(conn, map[string]any{
			"session_id": session,
			"type":       "listen",
			"state":      "stop",
		}); err != nil {
			fatal("listen stop: %v", err)
		}
	}

	report(conn, endOfSpeech, *replyFor)
}

// fetchOTA asks the cloud for the WebSocket endpoint and the activation state,
// mirroring the client the plan verified against. Identity is carried in the
// headers; the body is a stripped-down board info like the ESP32 sends.
func fetchOTA(url, deviceID, clientID, lang string) *otaInfo {
	body, _ := json.Marshal(map[string]any{
		"version": 1,
		"language": lang,
		"mac_address": deviceID,
		"uuid": clientID,
	})
	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		fatal("ota request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Activation-Version", "1")
	req.Header.Set("Device-Id", deviceID)
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("User-Agent", "techo5-m0/0.0.1")
	req.Header.Set("Accept-Language", lang)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal("ota: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fatal("ota: status %d", resp.StatusCode)
	}

	var r otaInfo
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		fatal("ota response: %v", err)
	}
	if r.Websocket.URL == "" {
		fatal("ota response has no websocket.url")
	}
	return &r
}

// pollActivation re-asks the OTA endpoint until the code stops coming back — the
// owner types it at xiaozhi.me while this loop runs.
func pollActivation(url, deviceID, clientID, lang string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			fatal("activation was not redeemed within %v", timeout)
		}
		time.Sleep(5 * time.Second)
		fmt.Println("waiting on the code at xiaozhi.me ...")
		info := fetchOTA(url, deviceID, clientID, lang)
		if _, ok := info.activation(); !ok {
			fmt.Println("activation redeemed")
			return
		}
	}
}

// dial opens the WebSocket with the headers the plan lists.
func dial(wsURL, token, deviceID, clientID string) (*websocket.Conn, error) {
	hdr := http.Header{
		"Authorization":     {"Bearer " + token},
		"Protocol-Version":  {"1"},
		"Device-Id":         {deviceID},
		"Client-Id":         {clientID},
	}
	c, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	return c, err
}

// hello sends the client hello and waits for the server's, returning the session
// id and the sample rate the server will use for TTS.
func hello(conn *websocket.Conn) (session string, srvRate int, err error) {
	if err := writeJSON(conn, map[string]any{
		"type":       "hello",
		"version":    1,
		"features":   map[string]any{},
		"transport":  "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	}); err != nil {
		return "", 0, err
	}

	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return "", 0, err
	}
	for {
		typ, data, err := conn.ReadMessage()
		if err != nil {
			return "", 0, err
		}
		if typ != websocket.TextMessage {
			continue
		}
		var m struct {
			Type        string `json:"type"`
			SessionID   string `json:"session_id"`
			AudioParams struct {
				SampleRate int `json:"sample_rate"`
			} `json:"audio_params"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Type == "hello" {
			return m.SessionID, m.AudioParams.SampleRate, nil
		}
	}
}

// captureAndSend records from the real microphone and pushes CELT frames,
// returning the time speech ended (the last frame sent).
func captureAndSend(conn *websocket.Conn, session string, seconds int, out string, bitrate, complexity int) time.Time {
	source, err := mic.Acquire()
	if err != nil {
		fatal("mic: %v", err)
	}
	defer func() { _ = source.Close() }()

	enc, err := opus.NewEncoder(opus.EncoderConfig{
		SampleRate: mic.Rate,
		Channels:   1,
		Bitrate:    bitrate,
		CBR:        true,
		Complexity: complexity,
	})
	if err != nil {
		fatal("opus encoder: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = source.Run(ctx) }()

	frames, unlisten := source.Listen("xiaozhi-m0")
	defer unlisten()

	pkt := make([]byte, 400)
	var acc []int16
	var wav []byte
	var frameCount, byteCount int
	deadline := time.After(time.Duration(seconds) * time.Second)
	var endOfSpeech time.Time

	fmt.Printf("capturing %d s of mic: start speaking now ...\n", seconds)

	for {
		select {
		case <-deadline:
			endOfSpeech = time.Now()
			if lag := len(acc); lag > 0 {
				fmt.Printf("dropped %d trailing samples (< one 60ms frame)\n", lag)
			}
		case frame, ok := <-frames:
			if !ok {
				endOfSpeech = time.Now()
			}
			acc = append(acc, frame...)
			wav = appendWav(wav, frame)
			if len(acc) < celtFrameSamples {
				continue
			}
			n, err := enc.Encode(acc[:celtFrameSamples], pkt)
			if err != nil {
				fatal("encode: %v", err)
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, pkt[:n]); err != nil {
				fatal("send: %v", err)
			}
			frameCount++
			byteCount += n
			acc = acc[celtFrameSamples:]
		}
		if !endOfSpeech.IsZero() {
			break
		}
	}

	actual := time.Duration(frameCount) * 60 * time.Millisecond
	fmt.Printf("pushed %d frames (%d bytes) = %.1fs of audio\n", frameCount, byteCount, actual.Seconds())

	if out != "" {
		if err := audio.WriteWAV(out, wav, mic.Rate, 1); err != nil {
			fatal("writing %s: %v", out, err)
		}
		fmt.Printf("wrote the pushed audio to %s\n", out)
	}
	return endOfSpeech
}

func appendWav(dst []byte, frame []int16) []byte {
	for _, s := range frame {
		dst = binary.LittleEndian.AppendUint16(dst, uint16(s))
	}
	return dst
}

// report reads text and binary frames until the server finishes answering, and
// prints the transcript and the end-of-speech to first-TTS-byte latency.
func report(conn *websocket.Conn, endOfSpeech time.Time, timeout time.Duration) {
	var sttText []string
	var firstTTSTime time.Time
	ttsState := ""

	fmt.Println("\n-- server messages --")
	deadline := time.After(timeout)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		typ, data, err := conn.ReadMessage()
		if err != nil {
			if wsErr, ok := err.(*websocket.CloseError); ok {
				fmt.Printf("closed: code %d (%q)\n", wsErr.Code, wsErr.Text)
			} else {
				fmt.Printf("read: %v\n", err)
			}
			break
		}

		if typ == websocket.BinaryMessage {
			if firstTTSTime.IsZero() {
				firstTTSTime = time.Now()
				fmt.Printf("first TTS byte +%v\n", firstTTSTime.Sub(endOfSpeech).Round(time.Millisecond))
			}
			fmt.Printf("  [tts bytes %d]\n", len(data))
			continue
		}

		var m struct {
			Type  string `json:"type"`
			State string `json:"state"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			fmt.Printf("  text: %s\n", data)
		} else {
			switch m.Type {
			case "stt":
				if m.Text != "" {
					sttText = append(sttText, m.Text)
				}
				fmt.Printf("  stt: %q\n", m.Text)
			case "llm", "tts":
				if m.State != "" {
					ttsState = m.State
				}
				if m.Text != "" {
					fmt.Printf("  %s: %q\n", m.Type, m.Text)
				}
				fmt.Printf("  %s state: %s\n", m.Type, m.State)
			default:
				fmt.Printf("  %s: %s\n", m.Type, data)
			}
		}

		if ttsState == "stop" {
			break
		}
		select {
		case <-deadline:
			fmt.Println("reply timeout")
			goto done
		default:
		}
	}
done:

	fmt.Println("\n-- result --")
	if len(sttText) == 0 {
		fmt.Println("no stt text received")
	}
	for _, t := range sttText {
		fmt.Println("transcript:", t)
	}
	if !firstTTSTime.IsZero() {
		lat := firstTTSTime.Sub(endOfSpeech)
		fmt.Printf("end-of-speech -> first TTS byte: %v (%s)\n", lat.Round(time.Millisecond), passFail(lat, 1500*time.Millisecond))
	} else {
		fmt.Println("no TTS received")
	}
}

func passFail(d, target time.Duration) string {
	if d <= target {
		return "PASS"
	}
	return "FAIL"
}

// writeJSON sends a frame in the shape every protocol message uses.
func writeJSON(conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, b)
}

// newUUIDv4 builds a random RFC 4122 v4 UUID in the dashed form the OTA
// endpoint requires ("Invalid client ID" without the dashes).
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		fatal("entropy: %v", err)
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// otaInfo is the part of the OTA response the throwaway reads.
type otaInfo struct {
	Websocket struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	} `json:"websocket"`
	Activation json.RawMessage `json:"activation"`
}

func (o *otaInfo) ws() (url, token string) { return o.Websocket.URL, o.Websocket.Token }

// activation reports whether the response still carries a code to redeem, and
// what it says. The cloud answers with a null object once a device is redeemed.
func (o *otaInfo) activation() (act struct{ code, message string }, ok bool) {
	var a struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if len(o.Activation) == 0 || string(o.Activation) == "null" {
		return act, false
	}
	if err := json.Unmarshal(o.Activation, &a); err != nil {
		return act, true
	}
	return struct{ code, message string }{a.Code, a.Message}, a.Code != ""
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "xiaozhi-m0: "+format+"\n", args...)
	os.Exit(1)
}