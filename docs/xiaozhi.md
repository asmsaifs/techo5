# Xiaozhi voice assistant

A second voice assistant on the device, next to Home Assistant. [Xiaozhi](https://xiaozhi.me) (小智) is
a voice-assistant service with its own agents and streaming speech recognition. You pick which one the
wake word reaches; the same words, a different one listening.

**Off until you turn it on.**

## What it sends, and to whom

While Xiaozhi is switched on and chosen, the microphone audio of each turn, and only that turn, is sent
to the Xiaozhi server. On the official cloud that is a commercial service outside your network, run by a
third party: it hears what you say, and it keeps whatever it keeps under its own terms, which are not
this project's to promise. The wake word is matched on the device and is not sent.

What else goes out: the device's id and a random client id, so the service can tell devices apart, and
the activation described below. The device sends no camera, no screen and no Home Assistant data. It
does not advertise the protocol's tool-calling feature, so the server cannot ask the device to do
anything.

If that is not what you want, point the device at a server of your own
([`xiaozhi_host`](actions.md#set-the-xiaozhi-server)), or leave the switch on Home Assistant.

## Turning it on

1. Turn on the **Xiaozhi** switch (Home Assistant), or choose it under Settings → Sound & Voice →
   Voice assistant.
2. The official cloud shows a six-digit **activation code** on the screen. Enter it in your account on
   xiaozhi.me. The device notices on its own within a few seconds.
3. Choose **Xiaozhi** as the Voice backend (Home Assistant's select, or the same settings row).

A server of your own has no activation step.

## Using it

Say the wake word. Say the request. The words heard and the reply appear on the screen. Ending an
answer: the action button, the stop word, or saying the wake word over it. Switching backend during a
turn ends that turn.

## Settings

| | |
|---|---|
| `xiaozhi` switch | Off keeps the client off the network entirely. |
| `voice_backend` select | Home Assistant or Xiaozhi. Home Assistant is the default. |
| `xiaozhi_host`, `xiaozhi_token`, `xiaozhi_client_id` | Actions, not entities: see [actions.md](actions.md#set-the-xiaozhi-server). |

## When it doesn't work

The diagnostics bundle has a **xiaozhi** section: whether the client is on, activated, connected, the
host, and packet counts. Token and client id show only as set or not. While a session is live the log
has a "still connected" line every 30 seconds.

| You see | Meaning |
|---|---|
| A code on the screen that never goes away | It has not been entered on xiaozhi.me yet. |
| Wake word gives an error tone | The client is off, still connecting or waiting for the code; the log says which. |
| An answer cuts off and the client reconnects | The link was silent for 5 s mid-answer. It retries with backoff from 5 s. |
