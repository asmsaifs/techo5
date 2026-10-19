# TECHO5 and similar projects

This page lists what TECHO5 and other projects cover. They all turn old smart displays, Echos and
tablets into Home Assistant voice satellites. It isn't a ranking. Each project has its own focus,
and each one helps keep good hardware out of the landfill.

It's a snapshot. Each project was checked on October 19, 2026, against its source code, docs and
release pages (linked at the bottom). The table describes each project's latest release. Work
that isn't released yet (on a main branch, another branch or an open pull request) is listed
under [In progress](#in-progress). These projects move fast, so some of this will be out of date soon. If something here is wrong or
missing, about TECHO5 or anyone else, please
[open an issue](https://github.com/HuskerMinion/techo5/issues) and I'll fix it. Thanks.

Where a cell says "not listed", I didn't find it in that project's docs or code. It doesn't mean
they can't do it. TECHO5 itself started from EchoLocal.

## At a glance

| | TECHO5 | EchoLocal | EchoMuse | echo-dot-assist | Kiosk Satellite | View Assist (with VACA) |
|---|---|---|---|---|---|---|
| **What it is** | Replacement firmware for Echos | Replacement firmware for Echos | Replacement firmware for the Echo Dot, plus a controller (a Home Assistant add-on or a container) | Keeps the Echo's own Fire OS and swaps only Alexa for its own small program (`hassmic`) | Android kiosk app for Home Assistant displays, with a built-in voice satellite. Also runs with no screen on an Echo Dot | Home Assistant integration, views and blueprints, plus an Android companion app (VACA) |
| **Devices** | Echo Show 5 1st and 2nd gen, Echo Show 8 1st and 2nd gen, Echo Spot 1st gen, Echo Dot 2nd gen, Echo 2nd gen | Echo Dot 2nd gen, Echo Show 5 1st and 2nd gen, Echo Show 8 1st gen. More in progress | Echo Dot 2nd gen. Ports in progress | Echo Dot 3rd gen (2018, D9N29T), Echo Dot 2nd gen, Echo 2nd gen. Echo Shows in progress. Not the 2019–2020 Dot 3 or the Dot 3 with a clock | Anything with Android 7.0 or newer. Guides for Fire tablets, Meta Portals, the Raspberry Pi 4 and the Echo Dot 2nd gen (on its own LineageOS image). Its docs also name Echo Shows on LineageOS, and tablets like the Galaxy Tab and Lenovo Tab | Android 8 or newer for VACA. Guides for the Echo Show 5 1st and 2nd gen and Show 8 1st gen (on LineageOS), Lenovo ThinkSmart View and Smart Displays, Sonoff NSPanel Pro 120 and Xiaomi Smart Clock. Voice PE and Atom Echo work as satellites without a screen |
| **What runs on the device** | Alpine Linux and one Go daemon (`echod`). No Android | One Go daemon (`echod`) and small helpers, on the Echo's own kernel and system, with Amazon's apps and Android's app framework stopped | emOS (its own small Linux userspace on the Dot's kernel), or rooted Fire OS 5 | Stock Fire OS with Amazon's audio front end and wake word engine, plus `hassmic`, a C daemon. A firewall keeps Amazon out | Android app | Android app (VACA, with its own web view), or another browser showing the views |
| **Install route** | Unlock (amonet) and LineageOS by hand on the Shows and the Spot, the Fire OS steps on the Dot, saiyaku-athena on the Show 8 2nd gen; the Echo 2nd gen needs its case opened. Then one Python install script over USB, on Windows, Linux or macOS. Nothing to build | Unlock to TWRP, then the `echolocal` installer over USB | Unlock with firebreak (amonet-biscuit), then a setup wizard in Chrome or Edge over USB | Unlock and root (micro-USB on the Dot 2, test pads on the Dot 3, soldering on the Echo 2), then one guided setup script on a Linux PC (not a VM or WSL). Each model needs one exact firmware | Install the APK and follow its setup wizard, on the device or from a browser. On an Echo it needs LineageOS; for the Echo Dot 2nd gen it has its own LineageOS image that installs the app on first boot | Install the APK and the two integrations (View Assist and VACA). On an Echo Show it needs LineageOS |
| **Wake word on the device** | Yes, microWakeWord: twelve words plus your own models; two wake words at once, each with its own assistant. A "stop" word with no wake word. Can also hand the room to Home Assistant's wake word engine (openWakeWord, say), or ask it to confirm | Yes, microWakeWord and openWakeWord, two assistants with their own wake word, a "stop" word. Add your own models through its integration | Yes by default, or on the controller per Dot. Custom words trained with oww_forge | Yes, Amazon's own (Alexa, and Echo, Computer and others downloaded from Amazon), with "Alexa, stop". Or microWakeWord (experimental), or Home Assistant's openWakeWord. Two wake words, each with its own assistant | Yes, vsWakeWord, microWakeWord and openWakeWord, with your own models for all three. Two wake words, each with its own assistant. A "stop" word. Works with the screen off or another app in front | Yes in VACA, microWakeWord or openWakeWord, built-in words plus your own |
| **Voice without Home Assistant** | Yes, if you want it: speech to text and text to speech on your own Wyoming servers, and any OpenAI-style chat model (llama.cpp, Ollama), with timers, alarms, the radio and web search | No. A small speech model on the Shows handles a few screen commands (off by default) | No (it needs Home Assistant) | No (it needs Home Assistant) | Partly: a realtime conversation goes straight to OpenAI, xAI or Gemini (or a relay on your network). The app itself expects Home Assistant | No (it's a Home Assistant integration) |
| **Home Assistant link** | ESPHome API, encrypted | ESPHome API, encrypted: Home Assistant sets the key when it adds the device, or you set one at install. Optional HACS integration | ESPHome API, through the controller. Its FAQ says this link isn't encrypted yet; the link from each Dot to the controller is | ESPHome API, encrypted. Or Wyoming, with fewer features | ESPHome API, encrypted, plus a long-lived token | Custom integration; VACA uses the Wyoming protocol |
| **Radio** | Yes, Radio Browser lists and favorites, with song and cover | Yes, a radio app on the Shows: Home Assistant's Radio Browser stations, with favorites | Yes, as a Home Assistant media player | Not listed | Through Music Assistant | VACA's media player plays Home Assistant's Radio Browser stations; a blueprint plays TuneIn through Music Assistant |
| **Music Assistant** | Yes, Sendspin, in sync as a group | Yes, Sendspin | Yes, Sendspin | Yes, Sendspin | Yes, Sendspin, in sync, with group volume, queue and speaker groups | Yes, through VACA's media player and blueprints |
| **AirPlay / Spotify Connect** | Both, on the Shows and the Dot (not the Spot). Off until you turn them on | AirPlay in progress. Has a Chromecast receiver (audio only on the Dot) | Not listed | Not listed | AirPlay (audio). Spotify Connect not built in, but it shows and controls a Spotify app playing on the device | Not listed |
| **Bluetooth** | Audio out to earbuds or a speaker, a phone playing in, a Home Assistant proxy that scans (no connections yet), iBeacon on the Dot | Bluetooth speaker on the Shows, proxy with iBeacon | Proxy, and up to three Bluetooth connections for Home Assistant | A phone playing in (SBC, AAC, aptX, aptX HD), audio out to a speaker, proxy with up to three connections | Proxy with active connections (locks, buttons, blinds), two or three at once | Bluetooth and USB microphones |
| **Other media** | DLNA speaker, video from `play_video` and DLNA, follow another player, lyrics, sleep sounds made on the device (white, pink and brown noise, rain, ocean, fire and more, in two layers), bass and treble, a sleep timer | Chromecast receiver for YouTube, Prime Video and more on the Shows, sleep sounds in two layers | Ducks music under replies, an 8-band equalizer, louder replies, the headphone jack | Amazon's equalizer (bass, mid, treble), extra speaker gain | DLNA for pictures, video and audio; follow a Home Assistant, Music Assistant or Sonos player, or another app on the device; lyrics; audio quality | Sound machine blueprint, separate voice, music and alarm volumes, music ducking |
| **Clock and weather** | Yes, 12 clock styles, 13 themes, in seven languages, forecast, rain radar, U.S. weather alerts | Yes, eight clock faces, weather, weather radar, U.S. weather alerts | No screen\* | No screen\* | Digital, flip and roller clocks with a night mode, an animated weather screensaver, weather and entity widgets | Clock and weather views |
| **Dashboards** | Drawn on the device (with a Rooms dashboard from your areas), or streamed exactly by dashcast, a server next to Home Assistant, which also shows web pages | Native dashboard, Tessera dashboards | No screen\* | No screen\* | Any Home Assistant dashboard, in its browser, with view rotation. Also your own HTML pages, and dashboard buttons that open its features or other apps | Any Home Assistant dashboard and View Assist's own views |
| **Screensaver and night** | Night hours (dark, a faint glow or a red night clock), photos behind the clock or as a screensaver, brightness that follows the room's light | Immich screensaver, a night clock that follows the room's light | No screen\* | No screen\* | Eleven screensaver modes (clock, weather, photos, cameras, a web page, a dashboard and more), a schedule by time of day, screen off after a while, brightness from its own or a Home Assistant light sensor | Screen timeout, auto brightness, a screensaver, rotating backgrounds; the screen wakes on motion |
| **Cameras** | Home Assistant and go2rtc cameras live, with sound, up to six at once on the Shows. Talk through a doorbell's speaker. Reolink doorbell chime with no Home Assistant. Its own camera to Home Assistant, or to an NVR over RTSP and ONVIF (not on the Show 8 2nd gen) | Its own camera over RTSP with ONVIF, a snapshot camera, a mirror view | No camera\* | No camera\* | Up to twelve live feeds per view, imported from Home Assistant or go2rtc, with pinch zoom, and sound on a single feed. Its own camera to Home Assistant, or over RTSP and ONVIF | Camera view; a doorbell can bring it up |
| **Photos** | Slideshow from anything Home Assistant's media library can browse (its media folder, network shares, Immich and other media sources), from Immich directly (an album, favorites, dates or a person), or photos kept on the device from the setup page. Behind the clock or as a screensaver, whole photo with blurred sides, two tall photos side by side, date and place captions | Immich screensaver, posters made from your photos | No screen\* | No screen\* | Immich (albums, people, tags, dates, videos), Home Assistant's media library and photos on the device. Two photos side by side, photo details | Rotating backgrounds from a folder, Unsplash or a link; community slideshow views and blueprints |
| **Calendar** | Yes: day and week on their hours, month, Coming up, icons, countdowns, the next event on the clock, event pop-ups. Home Assistant calendars or a calendar's own link (iCal) | Yes, a calendar app on the Shows (Home Assistant calendars, 3, 7 or 14 days), and to-do lists | No screen\* | No screen\* | No calendar screen of its own. Home Assistant's calendar cards work on its dashboards | Yes, "View Calendar" blueprint |
| **Alarms and timers** | Timers from Home Assistant (pause, add time); alarms that ring with Home Assistant down, sunrise light, light-only alarms, reminders by voice that go off on the devices you pick | Timers and alerts. Alarms in progress | Timers from Home Assistant, ringing up to 15 minutes | Timers from Home Assistant, ringing up to 10 minutes or until stopped. Alarms not listed | Timers on screen (pause, add time, from Home Assistant too); alarms that ring with no network, sunrise light, snooze, a spoken line, set by voice through an AI agent | Timers built into the integration; alarms, reminders and timers blueprint and view |
| **Intercom** | Yes: call one device from another, from the screen or Home Assistant. Works with Home Assistant down. Optional Drop In | Yes, audio on the Dot, audio and video on the Shows | Not listed | Yes, Drop In between Echos, with no cloud or Home Assistant in between | Yes, audio: call one kiosk or announce to all, push to talk, hands free or auto answer, from the screen, Home Assistant or by voice, kiosk to kiosk with no server in between | Broadcast messages to other satellites |
| **Nearest device answers** | Yes, off by default | Not listed | Yes, the loudest Dot answers | Yes, the Echos settle it among themselves. Can also work with Kiosk Satellite tablets | Yes, wake word arbitration | Not listed |
| **Announcements** | Yes, from Home Assistant, or device to device with no Home Assistant at all: typed words or your own voice in every room | Yes | Yes | Yes | Yes | Yes |
| **Notifications on the screen** | Yes, from Home Assistant: a card with an icon, a chime, a picture or a live camera, said aloud if you want | Yes, messages from Home Assistant pop up on the Shows | No screen\* | No screen\* | Yes, from Home Assistant: up to four cards at once, with an icon, a chime and a picture | Yes, a Device Alerts blueprint and an alert view, plus status icons |
| **Phone calls** | Through your own SIP provider, off until signed in | Not listed | Not listed (no calling) | Not listed | Not listed | Not listed |
| **Realtime voice** | Yes, OpenAI, xAI or Gemini with your own key | Not listed | Not listed | Not listed | Yes, OpenAI, xAI or Gemini, with Home Assistant's tools and web search | Not listed |
| **Stream deck** | Yes, TECHO5 Deck on the Shows: OBS scenes, stream, record and mute, plus shortcuts, apps and scripts on your computer | Not listed | No screen\* | No screen\* | Not listed | Not listed |
| **Presence** | Presence from the camera (the screen goes out when nobody's near), person detection. Off by default (no camera on the Show 8 2nd gen) | Not on the device itself. Its Bluetooth proxy and iBeacon feed room presence in Home Assistant (Bermuda, BPS) | Not listed | A motion sensor from the Wi-Fi signal (experimental) | Camera motion and face detection, a proximity sensor, the Portal's person sensor, or a Home Assistant motion sensor in the camera's place. Each can wake the screen or hold off the screensaver | Camera motion or face detection, a proximity sensor, a bump. Each can wake the screen |
| **Gestures** | A palm over the camera (not on the Show 8 2nd gen), a double clap, a hold on the clock. Off by default | Not listed | Button taps (one, two or three) and holds go to Home Assistant | Button presses and holds go to Home Assistant | Corner taps and holds, two and three finger taps and holds, a knock code, claps, fingers held up to the camera, the Echo Dot's button. Each runs an action you pick | Swipes with one to three fingers and L shapes go to Home Assistant; a quick actions sheet |
| **Sensors for Home Assistant** | Presence, person, room loudness, light, what's playing, ringing alarms and timers, CPU temperature and load, memory, free space, Wi-Fi signal | Room light, the camera covered, headphones, Wi-Fi, CPU and radio temperature, room loudness (Dot) | Room light, microphone muted, the action button | Sounds (smoke or CO alarm, breaking glass, a dog, a baby and more) from Amazon's model, a whispered request, Wi-Fi motion, room light, ringing timers | Battery, charging, CPU, memory, storage, light, room loudness, motion, person, GPS, network, the app in front, alarms, what's playing, a screenshot | Battery, charging, light, orientation, last motion, proximity, what was heard and said |
| **Kiosk lockdown** | A settings lock: a PIN before settings change, while alarms, volume and brightness stay open | Not listed | No screen\* | No screen\* | Kiosk mode (exit gesture and PIN, status bar, home and volume keys blocked), a lockdown mode for cleaning or guests, home launcher, app launcher, URL filter, device owner mode | Not listed |
| **Settings across devices** | Link everyday settings with the devices you pick (off until you turn it on), copy once, or save to a file and restore. No main device | Config sync, device to device | Fleet settings in the controller, changed per section on each Dot | Copy settings and wake word models Echo to Echo, or export to a file and import | Fleet management: a leader sends profiles, plugins and updates to the others. Backup and restore | A master config with changes per device |
| **Setup in a browser** | A setup page for every setting; a screenshot of the screen when you allow it | A status page | Setup wizard, and a dashboard with logs and voice history | A settings page on each Echo, let in by a press of its action button | Remote admin for every setting, with live screenshots, logs, a file manager and backups | Settings in Home Assistant, and a settings menu on the device |
| **Remote API** | Signed, off by default, can't touch privacy settings | Not listed | Not listed | Not listed | Yes: REST and WebSocket, a JavaScript API for pages, Android broadcasts | Not listed |
| **Plugins** | No | Not listed | Not listed | Not listed | Yes, a plugin manager and a plugin kit | Not listed |
| **Updates** | Signed, A/B with fallback, from Home Assistant's update card; stable or dev channel; automatic between 2 and 5 AM if you want | From Home Assistant or its own screen, stable or development channel | Over Wi-Fi from its dashboard, with automatic rollback, for the firmware and emOS | Signed, from your PC or from Home Assistant (release or beta channel); falls back if a new version fails its self test | From GitHub, your own server or an uploaded APK, with a Home Assistant update entity; for a whole fleet at once | Integration through HACS; VACA offers app updates on the device |
| **License** | MIT | MIT (its shared library, libcountertop, is AGPL-3.0) | MIT | MIT | CC BY-NC-ND 4.0 (free for personal, non-commercial use), with an exception that lets plugins use any license | View Assist: CC BY-NC 4.0. Integration repo: no license file found. VACA: Apache 2.0 |
| **Latest release** | v1.2.23, October 18, 2026 (TECHO5 Spot and TECHO5 Dot the same day) | 0.0.9, October 15, 2026 | v2.18.1 (controller 2.26.1), October 15, 2026 | 2026.10.17.151309, October 17, 2026 (beta builds through October 18, 2026) | 2026.10.29, October 18, 2026 | VACA v0.13.5, October 10, 2026; integration 2026.7.0, July 8, 2026 |

\* "No screen" and "No camera" mean the devices in that project's latest release have none. EchoMuse and echo-dot-assist have Echo Show work in progress (see [In progress](#in-progress)). On echo-dot-assist's Show branch the screen shows the light ring as a band and stays dark otherwise; EchoMuse's Show pull requests don't list screen features yet.

A few related projects, for context:

| | What it is |
|---|---|
| [Home Assistant Voice Preview Edition](https://github.com/esphome/home-assistant-voice-pe) | Home Assistant's own voice satellite hardware, on ESPHome. No screen. Latest firmware 26.9.0, September 17, 2026 |
| [Linux Voice Assistant](https://github.com/OHF-Voice/linux-voice-assistant) | A voice satellite for any Linux machine over the ESPHome API. Apache 2.0. v1.1.15, August 2, 2026 |
| [Wyoming Satellite](https://github.com/rhasspy/wyoming-satellite) | The older Wyoming voice satellite. Archived; its README points to Linux Voice Assistant instead |

## In progress

Work that's public but not in a release yet, as each project shows it on October 19, 2026. Plans
change, and it's each project's call what ships and when. Pull requests from people outside a
project are marked "community".

- **TECHO5**: nothing public outside the latest release.
- **EchoLocal** ([main since 0.0.9](https://github.com/ygelfand/echolocal/compare/0.0.9...main)):
  - On main: the Echo Show 8 2nd gen; AirPlay on the Shows, with a switch; alarms (repeat days,
    snooze, sunrise light, set from Home Assistant too); wake word rework and choice; beamforming;
    tuning per model and stereo on the Show 8 1st gen.
  - Early code for the Echo Spot, not announced yet.
  - Community pull requests: the Echo 2nd gen ([#64](https://github.com/ygelfand/echolocal/pull/64),
    draft), Sendspin encryption ([#34](https://github.com/ygelfand/echolocal/pull/34)).
- **EchoMuse** ([pull requests](https://github.com/wilbowes/EchoMuse/pulls)):
  - Ports, all community pull requests: the Echo 2nd gen
    ([#687](https://github.com/wilbowes/EchoMuse/pull/687), draft, working for testers), the Echo
    Show 8 1st gen ([#358](https://github.com/wilbowes/EchoMuse/pull/358), draft, voice working)
    and the Echo Show 5 1st gen ([#36](https://github.com/wilbowes/EchoMuse/pull/36)).
  - The Echo Dot 3rd gen (2018) runs in a
    [community fork](https://github.com/shortgame11/EchoMuse)
    ([#527](https://github.com/wilbowes/EchoMuse/issues/527)).
  - The Echo Spot is filed for later ([#991](https://github.com/wilbowes/EchoMuse/issues/991)).
  - On main: wake word detection at about half the CPU, dashboard updates.
- **echo-dot-assist**:
  - The Echo Show 5 1st gen and Show 8 1st gen on the
    [`checkers` branch](https://github.com/Gamer92000/echo-dot-assist/tree/checkers), each running
    on a tester's unit: a voice satellite on Amazon's own Fire OS, with the light ring shown as a
    band on the screen. No release builds yet.
  - Pull requests on that branch: the Echo Show 8 2nd gen
    ([#34](https://github.com/Gamer92000/echo-dot-assist/pull/34), tried on a real unit) and
    Kiosk Satellite next to it on the Shows
    ([#35](https://github.com/Gamer92000/echo-dot-assist/pull/35), draft).
  - On main and in beta builds: separate volumes for music, replies, sounds, timers and calls.
- **Kiosk Satellite** ([changelog](https://github.com/jxlarrea/kiosk-satellite/blob/main/CHANGELOG.md),
  Unreleased):
  - Plugin voice skins, an image-link screensaver, a large-room clap mode, the Echo Dot's volume
    buttons, and volume buttons that control a Music Assistant group.
  - A [community plugins directory](https://github.com/jxlarrea/kiosk-satellite-plugins).
  - The same author's
    [camera and Bluetooth for LineageOS on the Echo Show 5 and 8](https://github.com/jxlarrea/lineageos-echo-show-camera).
- **View Assist**:
  - The integration's
    [main since 2026.7.0](https://github.com/dinki/view_assist_integration/compare/2026.7.0...main):
    screen fades between views, sentence files per language, plugin intents.
  - A [rework into built-in intents](https://github.com/dinki/view_assist_integration/compare/main...dev-intent)
    (setting modes, changing views, do not disturb, the climate view), which its maintainer calls
    major changes.
  - VACA's [next release](https://github.com/msp1974/ViewAssist_Companion_App/compare/main...dev):
    camera streams, a Sendspin server choice, and audio input and output choices.
  - Android 7 support for VACA
    ([pull request](https://github.com/msp1974/ViewAssistCompanionApp/pull/58)).

## What each project focuses on

- **TECHO5** replaces the software on Echos with a small Linux and one program, with no Android left.
  Its focus is a full screen experience drawn on the device (clock, calendar, photos, cameras,
  dashboards), with voice that can also run without Home Assistant, phone calls through your own
  SIP provider, and a stream deck for OBS on the Shows.
- **EchoLocal** also replaces Amazon's software with one Go program, on the Echo's own system. It
  has a Chromecast receiver (YouTube, Prime Video and more on the Shows), on-device openWakeWord
  as well as microWakeWord, apps on the Shows (calendar, radio, weather radar, to-do lists, alarms),
  GPU audio visuals, and an integration with turn history, device groups and custom sounds.
  It moves fast: a lot has landed on main since its last release (see In progress).
- **EchoMuse** goes deep on the Dot: a controller with a web dashboard and voice history, the
  choice to detect the wake word on the controller, a trainer for your own wake words, echo
  canceling and noise suppression, fleet settings, a ten-second way back to the original boot
  image, Bluetooth connections for Home Assistant, and a porting kit for more Echo models.
- **echo-dot-assist** keeps Amazon's Fire OS and swaps only Alexa for its own program. That keeps
  Amazon's own echo canceling, beamforming and wake words, made for these microphones, and adds
  the Echo Dot 3rd gen (2018), sound detection from Amazon's own model (its docs call it a hint,
  not an alarm), whisper detection, AAC and aptX from a phone, a Bluetooth proxy with connections,
  and a motion sensor from the Wi-Fi signal (experimental). The trade-off, in its own words:
  Amazon's software still runs on the Echo, kept off the internet by a firewall; each model needs
  one exact firmware; the install needs a Linux PC; and in Wyoming mode most of the extras go.
- **Kiosk Satellite** runs on anything with Android 7: tablets, Fire tablets, Meta Portals, a
  Raspberry Pi 4, Echo Shows on LineageOS, and, with no screen, the Echo Dot 2nd gen. It shows any
  Home Assistant dashboard as is, in a real browser, and adds fleet management, a plugin manager,
  kiosk and lockdown modes, a remote admin and APIs, many gestures, and eleven screensaver modes.
  It moves fast too: a new release most days.
- **View Assist** works with any satellite, ESPHome ones like Voice PE included, and has a big
  library of ready-made voice blueprints: lists, sports scores, travel times, Wikipedia, where a
  person is, and more. With VACA on an Android display it shows its own views or any dashboard.
- **License.** TECHO5, EchoLocal, EchoMuse and echo-dot-assist are MIT (EchoLocal's shared library
  is AGPL-3.0). Kiosk Satellite (CC BY-NC-ND) and View Assist (CC BY-NC) are free to use at home
  but limit commercial use or modified builds. That only matters if you want to change and share
  their code.

## Choosing

- **You have an Android tablet, a Fire tablet or a Lenovo display**: Kiosk Satellite, or View
  Assist with VACA. No unlock. Both keep your dashboards exactly as they are. For a Meta Portal or
  a Raspberry Pi 4, Kiosk Satellite has guides.
- **You run several displays and want them managed as one, or locked down for guests**: Kiosk
  Satellite.
- **You want lots of ready-made voice skills on any satellite you own**: View Assist's blueprints.
- **You have Echo Dot 2nd gens**: EchoMuse (with a controller), EchoLocal, TECHO5 Dot,
  echo-dot-assist, or Kiosk Satellite on its own LineageOS image. Want Amazon's own wake word and
  echo canceling kept? echo-dot-assist.
- **You have an Echo Dot 3rd gen from 2018**: echo-dot-assist. EchoMuse lists its port as in
  progress, and a community fork of EchoMuse runs on it.
- **You have an Echo 2nd gen**: TECHO5 Dot or echo-dot-assist. EchoMuse and EchoLocal have ports
  in progress.
- **You have an Echo Show 5 or Show 8 1st gen and are fine with the unlock**: TECHO5 or EchoLocal
  turn it into a Linux device. Want to keep Android apps on it? LineageOS with Kiosk Satellite or
  VACA. echo-dot-assist and EchoMuse have work in progress for these.
- **You have an Echo Show 8 2nd gen**: TECHO5. EchoLocal and echo-dot-assist have it in progress.
- **You have an Echo Spot**: TECHO5 Spot. EchoLocal has early work on it.
- **You want voice without Home Assistant**: TECHO5, with your own speech servers and a local chat
  model. Kiosk Satellite's realtime voice talks to OpenAI, xAI or Gemini directly.
- **You want something that just works out of the box**: Home Assistant Voice Preview Edition.

## Sources

Each project was checked on October 19, 2026, against its latest release and its main branch.

- TECHO5 (v1.2.23): [README](https://github.com/HuskerMinion/techo5),
  [docs/overview.md](https://github.com/HuskerMinion/techo5/blob/main/docs/overview.md),
  [docs/getting-started.md](https://github.com/HuskerMinion/techo5/blob/main/docs/getting-started.md),
  [docs/setup.md](https://github.com/HuskerMinion/techo5/blob/main/docs/setup.md),
  [docs/actions.md](https://github.com/HuskerMinion/techo5/blob/main/docs/actions.md),
  [docs/dashboards.md](https://github.com/HuskerMinion/techo5/blob/main/docs/dashboards.md),
  [docs/phone.md](https://github.com/HuskerMinion/techo5/blob/main/docs/phone.md),
  [docs/deck.md](https://github.com/HuskerMinion/techo5/blob/main/docs/deck.md),
  [docs/video.md](https://github.com/HuskerMinion/techo5/blob/main/docs/video.md),
  [docs/install-athena.md](https://github.com/HuskerMinion/techo5/blob/main/docs/install-athena.md),
  [releases](https://github.com/HuskerMinion/techo5/releases),
  [TECHO5 Spot](https://github.com/HuskerMinion/techo5-spot),
  [TECHO5 Dot](https://github.com/HuskerMinion/techo5-dot)
- EchoLocal (0.0.9, and main at 51dd65e, October 18): [README](https://github.com/ygelfand/echolocal),
  [source](https://github.com/ygelfand/echolocal/tree/main/internal),
  [release 0.0.9](https://github.com/ygelfand/echolocal/releases/tag/0.0.9),
  [EchoLocal integration](https://github.com/ygelfand/echolocal-hacs) (v0.0.11, October 19),
  [libcountertop](https://github.com/ygelfand/libcountertop)
- EchoMuse (v2.18.1): [README](https://github.com/wilbowes/EchoMuse),
  [FAQ](https://github.com/wilbowes/EchoMuse/blob/main/docs/faq.md),
  [configuration](https://github.com/wilbowes/EchoMuse/blob/main/docs/configuration.md),
  [release v2.18.1](https://github.com/wilbowes/EchoMuse/releases/tag/v2.18.1),
  [releases](https://github.com/wilbowes/EchoMuse/releases)
- echo-dot-assist (2026.10.17.151309): [README](https://github.com/Gamer92000/echo-dot-assist),
  [user guide](https://github.com/Gamer92000/echo-dot-assist/blob/main/docs/GUIDE.md),
  [devices](https://github.com/Gamer92000/echo-dot-assist/blob/main/devices/README.md),
  [changelog](https://github.com/Gamer92000/echo-dot-assist/blob/main/CHANGELOG.md),
  [releases](https://github.com/Gamer92000/echo-dot-assist/releases)
- Kiosk Satellite (2026.10.29): [README](https://github.com/jxlarrea/kiosk-satellite),
  [docs](https://kiosksatellite.com/docs/),
  [Voice Satellite](https://kiosksatellite.com/docs/voice-satellite/),
  [Custom wake words](https://kiosksatellite.com/docs/custom-wake-words/),
  [ESPHome](https://kiosksatellite.com/docs/esphome/),
  [Events](https://kiosksatellite.com/docs/events/),
  [Android broadcasts](https://kiosksatellite.com/docs/android-broadcasts/),
  [Custom pages](https://kiosksatellite.com/docs/custom-pages/),
  [Dashboard links](https://kiosksatellite.com/docs/dashboard-links/),
  [Screen](https://kiosksatellite.com/docs/screen/),
  [Screensavers](https://kiosksatellite.com/docs/screensavers/),
  [Immich](https://kiosksatellite.com/docs/immich/),
  [Media player](https://kiosksatellite.com/docs/sendspin/),
  [DLNA](https://kiosksatellite.com/docs/dlna/),
  [Intercom](https://kiosksatellite.com/docs/intercom/),
  [Camera streams](https://kiosksatellite.com/docs/cameras/),
  [Device camera](https://kiosksatellite.com/docs/camera/),
  [Kiosk and lockdown](https://kiosksatellite.com/docs/kiosk/),
  [Home launcher](https://kiosksatellite.com/docs/home-launcher/),
  [Gestures](https://kiosksatellite.com/docs/gestures/),
  [Alarms](https://kiosksatellite.com/docs/alarms/),
  [Fleet management](https://kiosksatellite.com/docs/fleet/),
  [Updates](https://kiosksatellite.com/docs/updates/),
  [Remote API](https://kiosksatellite.com/docs/remote-api/),
  [JavaScript API](https://kiosksatellite.com/docs/js-api/),
  [Plugins](https://kiosksatellite.com/docs/plugins/),
  [Fire tablets](https://kiosksatellite.com/docs/fire/),
  [Meta Portal](https://kiosksatellite.com/docs/portal/),
  [Raspberry Pi 4](https://kiosksatellite.com/docs/raspberry-pi/),
  [Echo Dot guide](https://kiosksatellite.com/docs/echo-dot/),
  [releases](https://github.com/jxlarrea/kiosk-satellite/releases)
- View Assist (integration 2026.7.0): [README](https://github.com/dinki/View-Assist),
  [docs](https://dinki.github.io/View-Assist/),
  [Views gallery](https://dinki.github.io/View-Assist/docs/extend-functionality/views),
  [Automations gallery](https://dinki.github.io/View-Assist/docs/extend-functionality/sentences),
  [integration](https://github.com/dinki/view_assist_integration)
- View Assist Companion App (VACA v0.13.5): [README](https://github.com/msp1974/ViewAssist_Companion_App),
  [wiki](https://github.com/msp1974/ViewAssist_Companion_App/wiki),
  [releases](https://github.com/msp1974/ViewAssist_Companion_App/releases),
  [manifest (Wyoming)](https://github.com/msp1974/ViewAssist_Companion_App/blob/main/custom_components/vaca/manifest.json)
- Context: [Home Assistant Voice PE](https://github.com/esphome/home-assistant-voice-pe),
  [Linux Voice Assistant](https://github.com/OHF-Voice/linux-voice-assistant),
  [Wyoming Satellite](https://github.com/rhasspy/wyoming-satellite)
