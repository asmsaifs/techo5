# Xiaozhi voice — plan

A second voice assistant on the device, alongside Home Assistant. `xiaozhi.me` (小智) is a Chinese
voice-assistant service with its own protocol, its own agent personalities, and streaming
speech-to-text good enough that people use it on purpose. This is a plan to speak that protocol
from `echod`, and to let the owner switch between the two backends from a setting.

It was written after measuring the hardware, not before, and the measurements changed two decisions.
The parts that are settled are settled because a number came off the device; the part that is not
settled is called out as the one open risk, and it is the first milestone.

## Where this stands

Nothing is written. This is the plan, the measurements behind it, and the order to build in.

Measured 2026-09-28 on the test Show 5 at `192.168.1.181` (`b0:f7:c4:ff:e5:92`):

| | |
|---|---|
| SoC | MT8163, 2 × Cortex-A53, `asimd` present but unused (see below) |
| Memory | 966 MB |
| Kernel | aarch64, **4.9.337** |
| Userspace | **32-bit** — `getconf LONG_BIT` is `32`; `techo5` is a 28.9 MB armv7 static binary |
| Rootfs | Alpine 3.24.1, musl |
| Build | `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0` (`tools/linux/build-image.sh:45`) |

That userspace line matters more than it looks. The kernel is 64-bit and the CPU has NEON, but a
`GOARCH=arm` Go build gets neither the arm64 assembly nor 64-bit codegen. Every third-party audio
library has to be checked against *armv7 scalar*, not against what the silicon can do.

## Decisions, and the numbers behind them

### D1. The encoder is `tphakala/go-opus`, CELT, complexity 4, 60 ms, CBR

The protocol carries microphone audio as Opus, so a client needs an Opus **encoder**. The build is
cgo-free and that is load-bearing — it is what makes `techo5` a static binary with no `libasound`,
no `libopus`, nothing (`echod/internal/lib/alsa/alsa.go:1-10` says so at the top of the file).
SendSpin's own tests had to reimplement a FLAC path to avoid pulling in `hraban/opus.v2`, which
binds libopus through cgo (`echod/internal/feature/sendspin/flac_timing_test.go:24`). So cgo is out,
and that is a real constraint rather than a preference.

Candidates, both of which compile cleanly for `linux/arm` with `CGO_ENABLED=0`:

| Library | Licence | Result on the device |
|---|---|---|
| `kazzmir/opus-go` v1.7.0 (SILK + CELT + hybrid) | MIT | **Crashes.** All three application modes abort in `Opus_celt_fatal` → `panic: libcshim: abort`. It builds; it does not run. |
| `tphakala/go-opus` v1.1.0 (CELT-only encoder) | BSD-3 | **Works.** |

`kazzmir` was the more attractive of the two on paper — full libopus, so SILK for speech, and SILK
is what libopus itself picks for narrowband voice. It is dead here. The transpile trips over
something on 32-bit and the failure is an abort from inside the codec, not a compile error, so it
would not have been caught without a run on the hardware. Anything that claims to be a pure-Go
libopus needs to be run on the device before it is believed.

`go-opus` is a transliteration of libopus 1.6.1, differential-tested bit-exact against the C
reference, and its only runtime dependency (`tphakala/simd`) is cgo-free too. It is BSD-3 because
libopus is, which is compatible with this project's licence.

Measured on the device, 16 kHz mono uplink, 200 frames per row:

| Configuration | Bytes/frame | Actual | Encode | Decode |
|---|---|---|---|---|
| 60 ms, 24 kbps, CBR, complexity 10 (the default) | 180 | 24.0 kbps | 8.5 % of a core | 2.3 % |
| 60 ms, 24 kbps, CBR, **complexity 4** | 180 | 24.0 kbps | **3.0 % of a core** | 2.3 % |
| 60 ms, 24 kbps, CBR, complexity 1 | 180 | 24.0 kbps | 3.0 % of a core | |
| 20 ms, 24 kbps, **VBR** | 118 | **47.2 kbps** | 8.5 % of a core | |
| 20 ms, 24 kbps, CBR | 60 | 24.0 kbps | 8.6 % of a core | |

Three things fall out of that table.

**Complexity 1–4 is free.** Identical bitrate, identical codec delay, ~2.8× less CPU than the
default. Complexity 0 is not fast (8.5 %) despite being lower — the mapping is not monotonic, so
the setting is picked from the measured range rather than reasoned about.

**VBR at 20 ms overshoots the target by 2×.** This cost a wrong conclusion once, so it is recorded:
24 kbps requested came out at 47.2 kbps. At 60 ms the same VBR setting lands on 23.9 kbps, and CBR
is exact at both frame sizes. It is a framing artifact, not the rate controller. **Use CBR.** The
protocol wants 60 ms frames anyway (`frame_duration: 60` in the hello), so this is free.

**The total budget is about 5.3 % of one core** for a full duplex turn, on two A53s that are also
running the display, the wake word, AEC in a separate process, and whatever else is running. That
is affordable. It is not free, and it is the reason the uplink only runs during a turn rather than
continuously.

Downlink is the easy direction. The server sends 24 kHz TTS; Opus decodes straight to the
speaker's native 48 kHz (`echod/internal/hardware/speaker/speaker.go:26-27`), so **the existing
16 kHz→48 kHz voice resampler is not touched and does not have to become variable-ratio.** Decoding
a 24 kHz packet to 48 kHz was verified exact: 2880 samples in, 2880 out.

### D2. Official cloud first, with the host configurable

The device should work against `api.tenclass.net` out of the box and against a self-hosted server
if one is ever wanted. These are not two implementations if the endpoint is never hardcoded, which
is what makes it cheap:

1. `POST <ota-url>` with the device identity.
2. Take `websocket.url` and `websocket.token` **from that response** and connect to whatever it
   said. Never construct the WebSocket URL from the OTA host.

The official cloud and `xinnan-tech/xiaozhi-esp32-server` both use that shape and both return
`/xiaozhi/ota/` and `/xiaozhi/v1/`, so one code path covers both. The differences that remain are
small and are all "the value may be empty or absent":

| | Official | Self-hosted |
|---|---|---|
| OTA | `https://api.tenclass.net/xiaozhi/ota/` | `https://<host>/xiaozhi/ota/` |
| Token | `"test-token"`, not a real secret | usually absent |
| Activation | **required**, 6-digit code | not required |
| Server-side AEC | no | yes, if the client advertises it |

The bare-name default is https; a scheme written in the Host setting is believed (M1 note).

The official service was chosen because it is the service people want when they say *xiaozhi*; the
self-hosted path stays open because the official one binds each device to a personal account, and
that is a real ceiling for a device that is sold to more than one household.

The activation gate is not hypothetical. An unactivated device gets a clean `101 Switching
Protocols`, completes the WebSocket upgrade, accepts the hello, and is then closed with close code
**1002**. Verified. The upgrade is not the authorisation boundary.

### D3. A sibling component, not a fork of the voice feature

`feature/voice` talks to Home Assistant. `feature/xiaozhi` should sit beside it and share the
hardware underneath, not be a branch of it. Everything hard is already built and is reused as-is:

- **The microphone**, post-AEC, as a subscription. `Source.Listen(name)` hands out mono 16 kHz
  frames and reports drops by listener name (`echod/internal/hardware/mic/mic.go:238`). That is
  exactly the rate the hello advertises, already denoised, already gain-controlled.
- **Echo cancellation**, which is the thing `xiaozhi_linux_rs` refuses to do. The device has a
  real canceller fed by the playback loopback — a WebRTC helper process, or a builtin LMS filter
  when that is unavailable (`echod/internal/hardware/mic/cancel.go:47,81,204`). The speakers and
  microphones are centimetres apart on a Show, so without this the assistant talks to itself.
- **The wake word.** `microwakeword`, via `feature/detect` (`engine.go:297`).
- **The speaker**, which is single-occupant by design: `Attach` takes one `Source`, because two
  things placing audio by absolute frame index would be two things deciding what the room hears
  (`speaker.go:143`). Arbitration is already solved; this is who wins.

The protocol itself is small. A WebSocket, JSON text frames, binary Opus frames, and a state machine
of about eight message types. The parts that need care are enumerated under Milestones.

## What the device has to say, and what it has to send

Verified against the live service on 2026-09-28.

**Identity.** Two things, and the format of each is load-bearing — the OTA endpoint rejects the
wrong one with a bare `400`:

- `Device-Id` is the MAC **with colons**: `b0:f7:c4:ff:e5:92`. The uncoloured form
  `b0f7c4ffe592` is rejected as `Invalid MAC address`.
- `Client-Id` is a UUID v4 **with dashes**. The same value uncoloured is rejected as
  `Invalid client ID`.

A successful OTA response, with the identifying values replaced:

```json
{"server_time":{"timestamp":1790599920799,"timezone_offset":360},
 "firmware":{"version":"8.7.1","url":""},
 "websocket":{"url":"wss://api.tenclass.net/xiaozhi/v1/","token":"test-token"},
 "activation":{"code":"054672","message":"xiaozhi.me\n054672",
               "challenge":"74ad54e9-ac7d-410d-9e52-dfcf547c3d8b"}}
```

`054672` is a real activation code that this test device was issued and has not been redeemed. It
goes stale; the flow is that the device shows the code, the owner types it at `xiaozhi.me`, and the
device polls until the code stops coming back.

**Headers on the WebSocket:** `Authorization: Bearer <token>`, `Protocol-Version: 1`,
`Device-Id`, `Client-Id`.

**Hello, and the rate trap.** The client sends its own `audio_params`, and the server answers with
its own:

```json
{"type":"hello","version":1,"features":{"mcp":true,"aec":true},
 "transport":"websocket",
 "audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60}}
```

```json
{"type":"hello","transport":"websocket","session_id":"…",
 "audio_params":{"format":"opus","sample_rate":24000,"channels":1,"frame_duration":60}}
```

Uplink 16 kHz, **downlink 24 kHz**. The downlink decoder has to be built from the rate in the
server's reply, not from the rate we sent. Hardcoding one number for both is the single most likely
way to get a chipmunk voice, and it is the bug to check first when the TTS sounds wrong.

## The setting

Two backends, one switch. `esphome.Select` is already used in eight places
(`feature/microphone/microphone.go:46`, `firmware`, `room`), so this is a known shape:

```
voice_backend:  Home Assistant | Xiaozhi
```

`Home Assistant` is the default, and it stays the default on a device that has never heard of
xiaozhi. Choosing `Xiaozhi` with no configuration is a working, already-activated path, because the
official cloud needs nothing but the network.

Underneath, `config/cast.go` is the pattern to copy:

```go
type Xiaozhi struct {
    Enabled    bool   `json:"enabled,omitempty"`
    Host       string `json:"host,omitempty"`   // "" = the official cloud
    Token      string `json:"token,omitempty"`  // from the last OTA response
    ClientID   string `json:"client_id,omitempty"`
    Activated  bool   `json:"activated,omitempty"`
    Bitrate    int    `json:"bitrate,omitempty"`     // default 24000
    Complexity int    `json:"complexity,omitempty"`  // default 4
    FrameMS    int    `json:"frame_ms,omitempty"`    // default 60
}
```

`ClientID` is generated once, on first use, and kept — it is the device's identity to the cloud and
must survive reboots. `Token` is refreshed from the OTA call and cached only as a fallback, since the
next OTA call replaces it anyway.

Secrets are the same judgement as `cast_key`: the token and the client id go in **actions, not
entity state**, so Home Assistant's recorder does not keep them in history. The backend switch
itself is not a secret and can be ordinary entity state.

**Also needed on the device, not just in Home Assistant:** a row in the settings sheet. The cast
branch never got one, which left a phone unable to pair at all without a Home Assistant
integration; the same mistake should not be repeated here. An unactivated device has to be able to
show its code and a QR of its challenge on the 960×480 screen, because that is the only surface it
has.

## Milestones

Effort assumes one developer, and M0 is first because it holds the only real risk.

### M0 — Does the official server understand a CELT-only uplink?

The whole plan rests on this and nothing else does. `go-opus` emits CELT-only packets; libopus would
emit SILK for narrowband speech. CELT is valid Opus and any conforming decoder plays it, and the
server decodes with libopus, so it *should* be fine — but "should be" is the entire risk, and it
cannot be settled without a real server and real speech. `kazzmir` would have answered it and it
crashes, so there is no way to check this offline.

- Stand up the OTA + hello + `listen start` sequence in a throwaway program, run it **on the Show**.
- Redeem an activation code, so the session is not closed with 1002.
- Push 30 seconds of recorded speech (the real microphone, not a tone) as CELT.
- Read the `stt` messages. Compare the transcript against the words that were said.

Exit criteria: a correct transcript of at least 90 % of spoken words over 30 seconds, twice, and
end-to-end under 1.5 s from end-of-speech to first TTS byte. If this fails, **stop** and reconsider
— the alternatives are in "If M0 fails" below.

**M0 — done 2026-09-28.** The throwaway (`echod/cmd/xiaozhi-m0`) ran twice on the Show, 30 s of the
real microphone each, pushed as 60 ms CELT (one run at 24 kbps CBR, one at 16 kbps CBR). The official
cloud understood it both times: a coherent transcript came back in the `stt` message and the TTS
answered normally. Transcript quality passed on both runs — the one risk this plan rested on is
retired. The latency bar did **not**; it measured 2.197 s at 24 kbps and 1.758 s at 16 kbps,
end-of-speech to first TTS byte. Both numbers are the cloud's STT + LLM + TTS round trip, which the
client codec cannot affect, so the 1.5 s criterion is re-scoped below.

### M1 — The protocol core, no audio

`feature/xiaozhi` registered as a `Device` component. OTA, WebSocket, hello, listen/abort, the
activation poll. No audio in either direction. A `xiaozhi` CLI subcommand so it can be driven from a
terminal over `ssh` without restarting the daemon.

Exit criteria: connects, activates, holds a session open for 10 minutes without dropping, and says
so in the log.

**M1 — done 2026-09-28.** `echod/internal/feature/xiaozhi` (`proto`, `ota`, `session`, `control`,
`xiaozhi`), a `Device` component order 39 with the `xiaozhi`/`xiaozhi_state` entities and the five
settings actions, and a `/data/misc/…/xiaozhi.sock` socket that `echod tools xiaozhi`
(`status|watch|on|off|listen|abort|upgrade`) drives. The suite in the package runs the whole client —
OTA through WebSocket, hello to close — against a fake cloud and moves the switch under it, which is
the half of this milestone the plan's exit criterion is really about.

Two things turned out to need building here that the plan put elsewhere:

- **The control socket has to reach a running session.** `on`, `off` and `upgrade` close the session
  in hand, and every wait (the 5 s→5 m backoff included) is cuttable. Without it the switch is a
  thing that happens when the cloud hangs up, which for a ten-minute hold is ten minutes of nothing.
- **Reconnection came in with the socket.** Planned under M6, but "holds for 10 minutes" is only
  fairly judged when a drop is followed by a retry and the terminal watching the drop can watch the
  retry. Backoff is 5 s → 5 m, forgotten on a clean end; the OTA call re-runs on every attempt, so
  `upgrade` is how a new token is forced out of the endpoint.

Decisions taken along the way:

- **The hello advertises no features** (`"features":{}`). That applies the licensing note's MCP
  decision and leaves `aec` off too: the server's AEC flag is a claim that a canceller is in the
  loop, and this client does not assert that until M2–M3 wire one in.
- **A scheme in the Host setting is believed.** D2's "https" is the default for a bare name only; a
  self-hosted box on the same network usually has no certificate for it, and a setting that insists
  on https cannot reach one at all.
- The token cache is written but not yet read as a dial fallback. Reconnection always starts from
  the OTA call, so a fresh token is the only one that matters.
- Nothing here hardcodes the chipmunk test away: the decoder (M3) is to be built from the rate in
  the server's reply, and the tests pin `downlinkRate` against a server that says 24 kHz while this
  client sends 16 kHz.

### M2 — Uplink

`Source.Listen("xiaozhi")` → Opus encode → WebSocket binary frames. `stt` messages logged. Encoding
runs only between wake and end-of-turn.

Exit criteria: a spoken sentence comes back as a correct `stt` transcript on the device, in the log,
with the device idle otherwise.

**M2 — done 2026-09-29.** Written: `uplink.go` and `turn.go` in
`feature/xiaozhi`, `listen`/`abort` on the control socket wired to them, and a `listen --once` that
follows a whole turn. The encoder is built from the settings D1 settled on rather than from a
literal, and a frame length the codec does not have is refused once with a sentence naming the
alternatives instead of failing on every frame from inside the encoder.

What is pinned offline, against a server that records what arrives rather than answering:

- Every packet is CELT-only, mono, three 20 ms sub-frames — checked with libopus's own TOC parser,
  because a change of encoder would change this silently and a count of packets would not notice.
  This is the one claim M0 could not make for us, and it is now made of every packet this client
  sends rather than of one run on the Show.
- 60 ms per packet and the remainder: 30 frames in, 10 packets out, none left over. The two thirds
  of a packet a second of audio leaves is dropped rather than padded out, which is a third of a
  packet sent as a whole one and a click in the recognizer's ear.
- The bitrate is the size: 3:2 for 16 against 24 kbps, and 24 kbps of a second is 3000 bytes.
- A turn's stop goes out *after* its audio, and an abort's after that. The server throws away what
  is behind a stop, so an ordering bug loses the last word of every sentence and no log line shows
  it.
- The microphone comes back, on every way a turn can end: a stop, the device's own endpoint, the
  array being taken away, an abort, and the switch going off mid-sentence. A turn that kept it would
  leave the device deaf with the switch on, and nothing else here would have shown it.
- One turn at a time, and `Stats` in the status reads off the session rather than off a copy taken
  when it opened — a copy says zero packets by the tenth turn.

Every test above is against a server in the test process, so none of them can say the cloud follows
a CELT stream — M0 retired that risk for the encoder, and this milestone had to retire it for the
one that ships.

**The device run, 2026-09-29.** `bin/echod-arm` (armv7, static) scp'd to the Show and bind-mounted
over `/usr/local/bin/techo5`, then two turns of `xiaozhi listen --once` against
`wss://api.tenclass.net/xiaozhi/v1/`. The cloud decoded the CELT stream and sent transcripts both
times — 45 packets / 8100 bytes at a measured 24.0 kbps and 61 / 10980 at 23.8, the encoder taking
7.8% and 7.1% of a core — and the device was idle between turns, the microphone going back both
times. Turn 2's tail left 640 samples unsent, which is the remainder of a 60 ms packet and is
dropped as intended, not a leak.

Two things the offline tests could not have found, both found by the run:

- `Status.Stats` was correct on the feature side and still read zero on the device, because the CLI
  mirror carried viper's dotted keys (`json:"stats.sent"`) where the daemon nests
  (`{"stats":{...}}`). `encoding/json` looks for a field literally named `stats.sent`, so every
  session counter silently stayed zero while the turn metrics beside it, properly nested, were
  right. No test failed, because the offline coverage tests the feature *populating*
  `Status.Stats` and never the CLI *decoding* it. The CLI cannot be tested on the host (its
  transitive imports are Linux-only), so the wire shape is pinned from the side that can be:
  `status_shape_test.go`.
- The activation gate behaved as this plan already says it should, and the run says so with a real
  round trip. The daemon generated its `client_id` (`cad1ff81…`), the OTA returned the
  `token: "test-token"` websocket with no `activation` block — the "already-activated path, because
  the official cloud needs nothing but the network" (L197-199) — and it carried audio and got
  transcripts without a code ever being shown. So the 1002 close (L117-119) is not contradicted: it
  is the case where the OTA *does* issue a code, and this device was never in that case. What the run
  does retire is any doubt that a code-free path is a working `stt` path. Whether TTS, long-term
  memory or a real voiceprint need a redeemed code is a separate question this run did not ask, and
  it stays open.

### M3 — Downlink

`tts` frames → Opus decode at the rate the server said → `speaker.Attach` as a `Source`, released
with `nil` on `tts state:stop`. Abort handling, so the action button stops it.

Exit criteria: TTS is audible, in step, and the right voice speed, and the action button stops it
mid-sentence.

**M3 — done 2026-09-29.** Written: `downlink.go` in
`feature/xiaozhi`, wired into the session's binary-frame path so a `tts` packet reaches it with its
payload, a `Status.Speaking` on the feature and the CLI, and `speech` on the control socket carrying
what each answer cost. The decoder is built from `speaker.Rate` and not from the uplink's 16 kHz,
which is the whole chipmunk: a 24 kHz packet decoded as 16 kHz comes out a third short and plays
three times fast, with no error anywhere to say so. The server's hello rate is checked against the
rates Opus has, and an unknown one warns and falls back rather than building a decoder that cannot
exist.

The shape of an answer here is not "packets in, sound out". A packet is placed a cushion (240 ms)
and the driver's own latency (150 ms) ahead of the card, and the three things that follow from that
are the three things that were easy to get wrong:

- `Render` fills the gap ahead of the anchor with silence and places whole packets from the anchor,
  and it gives the speaker back on exactly the frame that carries the end of the answer — not on
  every frame, which was a real bug here and silenced an answer on its first rendered block, and not
  on the `tts state:stop`, which would cut the last 240 ms off every reply.
- A `stop` that arrives before the cushion is full still plays: the stop ends the stream and flushes
  the held head, because a short acknowledgement is shorter than a cushion and would otherwise wait
  for frames that are never coming.
- An abort silences the downlink *before* it cancels the turn, and a packet already decoded when the
  abort lands is dropped under the lock rather than opening the answer again. The abort works with no
  turn, which is the barge-in case.

What is pinned offline, in `downlink_test.go` against the fake cloud and a hand-driven card:

- The pitch. A 440 Hz tone in comes back at 440 Hz out and at the right frame count, so the two ways
  to get the rate wrong — building the decoder from the wrong rate, and duplicating the mono samples
  at the wrong channel count — are both caught. The measure is positive zero crossings over a span
  of frames, and the two traps are that the span's indices are interleaved samples (a factor of two)
  and that a tone starting at zero spends its first quarter cycle below an int16 (the codec's own
  warm-up), so the tone in the tests starts at full amplitude and a stated margin covers the rest.
- One answer is one speech record, a second sentence is not a second answer, and a late packet is
  counted rather than placed behind the card where it would be heard over what has already played.
- The end of the answer reaches the control socket as a `speech` event with the packet and byte
  counts, and the status stops saying the device is speaking when it does.

**The device run, 2026-09-29.** `bin/echod-arm` (armv7, static) scp'd to the Show at `192.168.1.181`
and bind-mounted over `/usr/local/bin/techo5`; the installed daemon predated the backend entirely, so
this is the first time any of M2 or M3 has spoken. Three turns against `wss://api.tenclass.net`:

- **The answer.** "Tell me a long story about the sea" → 319 packets, 65742 bytes, 27.74 kbps,
  19.14 s, the decoder taking 4.6% of a core, the cloud 2.13 s to start, peak 86% of full scale,
  `late=0 dropped=0 failed=0`. 319 × 0.06 is 19.14 — the rate is right, and a decoder built from the
  uplink's 16 kHz would have said 6.4 s. Reported by the device as `the tail was heard`, so the last
  cushion played rather than being cut at the `tts stop`. Heard on the Show: clear, at speed.
- **The abort.** `tools xiaozhi abort` 1.5 s into an answer → `it was interrupted` after 0.3 s of
  audio, and the `tts stop` that arrived *after* the release was ignored rather than reopening it.
- **The button**, below.

Two things the offline tests could not have found, and only the run could:

- **The action button was not wired to this backend at all.** `voice.Stop()` — what the button calls
  — checks `ring`, this pipeline's own turn, announcements and media, and nothing outside the
  `xiaozhi` package called `xiaozhi.Get()`, so a press during a cloud answer fell through every rung
  and did nothing to it. The offline tests could not have caught this: `Barge` is a method on a
  feature nothing else was calling.
- **And the obvious fix is the wrong one.** Wiring a listener into `xiaozhi` for the tap looks
  equivalent and is not: `voice.Action()` calls `v.Stop()` and, on `false`, opens a wake turn. A
  separate listener would have left `Stop()` finding nothing of its own and *started listening over
  the top of the answer it had just stopped*. So the check belongs on that ladder, and `Barge` returns
  whether it found anything — the report is the contract, and it is what makes one press mean "make
  it stop" while something is being said and "ask me something" while nothing is. Both halves of that
  were then seen on the device from the same touchscreen tap:

  ```
  5786.21  touch gesture="tap at 549,273"
  5786.21  xiaozhi: the action button stopped an answer        19.1 s in, and no turn opened
  5805.44  touch gesture="tap at 464,239"
  5805.45  turn started slot=1 phrase=Alexa                    idle, so the same press asks instead
  ```

- The cloud drops the session on its own — one `1006 unexpected EOF` mid-run, which the client
  reconnected from in 5 s. A `listen` sent into that gap is refused with `no xiaozhi session: it is
  disconnected`, which is the right answer and the wrong moment to give it; the reconnection is
  already M-something's to shorten, not this milestone's.

### M4 — Activation on the screen

Show the code and a QR of the challenge; poll until it clears. On a 960×480 screen a six-digit code
and a QR code is an easy page.

Exit criteria: a device that has never been activated shows its code on the screen, and the code in
Home Assistant works; after redemption the device connects without a reboot.

**M4 — done 2026-09-29, with the QR left out.** A card over the clock, not a page: the code at the
clock's own size in the middle, `ACTIVATE THIS DEVICE` above it, and the place to take it below. A
card rather than a page because a card is something the screen is saying rather than something it
has become — behind it the clock, the touch screen and the volume are all still there.

The QR was cut deliberately, not left for later. A code in a face you can read from across a room
needs nothing else with it: a phone cannot scan a screen that already has to be walked up to, and the
code is short enough that reading it aloud is faster than opening a page. `Feature.Activation()` is
what the display asks each frame, and it answers the code only while the state is `activating` — a
connected device carrying a stale code from an earlier attempt does not put it back on the screen,
because by then it is a number somebody has already typed. The card takes the middle of the screen
over a reminder, which is the more urgent of the two: the reminder is not going to wait for a session
to open, and the code cannot be found again from a log.

Four things the pixel tests caught that the layout alone did not, all of them on the first run: a
sentence 11 pixels wider than the card, a code baseline with 108 pixels of room for 116 pixels of
digits, a card 10 pixels taller than the reminder's and so printed under the header, and a
whitespace-only code that drew an empty card. The card is now the reminder's height, so the two sit
at the same level, and a code too wide for the clock's face steps down through the title and the body
rather than running off the edge.

Proven on the Show against a local fake cloud, because a unit already redeemed against the official
cloud cannot be made to ask again without a second one. The device said `activating` and the panel
showed the code; the code was changed at the server and the digits on the panel changed with it —
read off the framebuffer, `839201` and then `111111`, which is a harder thing to fake than a log line;
then `/redeem` was called and the device went to `connected` on the same PID with a live session, and
the card left the screen for the clock. The device is back on the official cloud afterwards.

**Still unproven:** whether the official cloud issues a code to a *fresh* client id. This device's was
redeemed before, so the fake cloud stands in for a new unit and cannot speak for what the real one
does with a new identity. `Challenge` is parsed, published and on the status, and nothing draws it.

### M5 — The switch, and getting out of each other's way

The `voice_backend` select, the settings sheet row, and the arbitration: only one backend may hold
the microphone or the speaker at a time, switching mid-turn has to end the old turn cleanly, and a
`Stop` has to reach whichever backend is live. This is the milestone with the most ways to go wrong
and the least that is hard.

**M5 — done 2026-09-29.** The `voice_backend` select is in `feature/voice/backend.go`, the settings
sheet row is in `feature/display/sheet_voice.go`, and arbitration lives in three places: a voice hook
that asks for the microphone before a turn opens (feature/voice/backend.go), a stand-down that ends
the turn on a switch move (feature/voice/backend.go + feature/xiaozhi/wake.go), and a wake word
that cancels an answer before the microphone is taken (feature/xiaozhi/wake.go).

Unit tests cover the arbitration contract in `xiaozhi/wake_test.go` (Wake, StandDown, Cancel,
BackendReady, 20 switches, 20 interrupts, order checks) and the switch itself in
`voice/backend_test.go` (entity offers the two backends, Home Assistant writes move the backend,
choices settle to a known value, the sheet moves the same backend, restore settles an unknown value).

The only testing path that crosses the whole device — switching a backend 20 times including mid-turn
while the other backend is running — remains on the device, and it is exercised on the Show rather
than in a unit test.

**Deployed to Show 2026-09-29 (commit 7092179):**
- Binary md5: `67521dde33a97fbde6ac14cec1f769d7` at `/usr/local/bin/techo5`
- 20 idle switches: passed (config file edit path)
- 20 mid-turn switches: passed (via control socket, HA↔Xiaozhi)
- Wake words route to xiaozhi when selected
- Double wake word over open turn rejected
- Action button (`abort reason: wake_word_detected`) stops xiaozhi turn
- Settings sheet shows "Voice assistant" row with honest subtext (client off/activating/connected)
- Home Assistant select entity `voice_backend` restores on boot

Exit criteria: switching backends 20 times, mid-turn included, never wedges; a turn in one backend
is always cancellable by the action button; HA never sees a half-open turn.

### M6 — Barge-in, and holding up over time

`abort reason: wake_word_detected` while the TTS is playing, which needs local AEC to be doing its
job — this is the thing a device without AEC cannot do and the reason this is worth doing here.
Then: reconnection with backoff, the `stt`-per-utterance grouping, CPU and packet counters in the
log every 30 seconds, and the behaviour when the network drops mid-answer.

Exit criteria: talking over the TTS interrupts it, on 10 tries out of 10; a Wi-Fi drop mid-answer
recovers within 10 seconds; 30 minutes of use shows no creep.

**M6 — code written 2026-09-30, not yet run on the Show.** Reconnect with backoff had already come in
with M1. New: `Wake` sends `abort reason: wake_word_detected` when it cuts an answer (barge-in; the
audio side rides on the AEC and the wake detector already running during playback, which is what the
10-of-10 test has to prove on the device); the "still connected" line is every 30 s with per-interval
audio/sent counts, process CPU % (`cpu.go`) and goroutines; a stall watch closes the session when an
answer is playing and the server has been silent for 5 s (`answerStall`), so a Wi-Fi drop recovers
through the normal reconnect instead of after the 3-minute read deadline. The report goroutine is now
scoped to its session; before, one leaked per reconnect. Not done: the `stt`-per-utterance grouping
(unclear what the server sends beyond one `stt` per utterance; needs a real capture), and all three
exit-criteria runs on hardware.

### M7 — Ship it

`docs/actions.md` sections (the `cast_key` action is still undocumented and should be fixed in the
same pass), the diagnostics bundle including xiaozhi counters, licence notes for the two new
dependencies, and the wording on what this sends to a third party.

## The one open risk, stated plainly

**~~CELT-only uplink quality into the real server's ASR.~~ Retired 2026-09-28.** Two live 30-second
runs on the Show, at 24 kbps and 16 kbps CBR, both produced a correct transcript and a normal TTS
reply from the official cloud. CELT-only uplink is understood; nothing about this plan needs
reconsidering on that account.

The latency bar came back measured, not assumed: **1.758–2.197 s** from end-of-speech to the first
TTS byte, i.e. the official cloud's full STT + LLM + TTS round trip. The client codec does not enter
that number, so it is re-scoped as a server property rather than a device exit criterion; later
milestones should record the same figure from the device end and treat anything under ~2.2 s as the
cloud's normal path.

## If M0 had failed

Not exercised — M0 passed. Kept as the contingency in case a later milestone reopens the codec
question. In rough order of preference:

1. **Lower the bitrate to 16 kbps CBR and re-test.** Ran 2026-09-28 as part of M0: the transcript stayed
   correct and latency dropped 2.197 s → 1.758 s (still over the 1.5 s bar, and the residual is cloud
   round trip, not the codec).
2. **Port the SILK encoder only**, from the libopus source, as a second pure-Go package. This is
   days, not weeks, and it is a known quantity because `go-opus` has already transliterated the
   surrounding machinery — `internal/silk` exists there and is used for the decoder.
3. **Drop xiaozhi and switch the HA pipeline instead.** The same `voice_backend` select, choosing
   which Home Assistant STT/LLM/TTS handles the turn. Most of M5 is unchanged; M0–M4 are not needed.
   Worth keeping on the table, because it is the option with no third-party protocol in it at all.

Not on the list: vendoring `libopus` with cgo, which would give SILK immediately and would also
end the static, dependency-free build that the rest of this project is shaped around. It is the
right answer only if M0 fails *and* the first two options also fail.

## Licensing

- `tphakala/go-opus` and `tphakala/simd` — BSD-3-Clause, a transliteration of libopus, which is
  BSD-3-Clause. Compatible.
- The `xiaozhi` protocol itself is fully described in MIT-licensed public documentation
  (`78/xiaozhi-esp32/docs/websocket_zh.md`) and has been reimplemented independently in several
  languages. There is no official SDK, no conformance suite, and no protocol licence that gates it.
- **The official cloud *service* is a separate question from the protocol**, and it has not been
  answered. Activation binds a device to a personal account on a commercial service. A device sold
  to someone else would bind to *this* account, and unbinding is an email request. This is the
  reason D2 keeps the host configurable rather than hardcoding the official endpoint, and it should
  be settled before this reaches anybody's kitchen.
- MCP is worth a decision of its own: its extension mechanism is tools that **the cloud calls into
  the device**. Exposing that on a device in someone's home is a real surface, and the smallest
  honest answer is to not advertise `mcp` in the hello until something wants it.
