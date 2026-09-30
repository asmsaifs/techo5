# Setting it up

What to set up once TECHO5 is installed and the device is in Home Assistant, in the order that
works. [Getting started](getting-started.md) gets you to that point. Everything here can be changed
later, and most of it is optional.

Photos, weather, cameras and the night settings are for devices with a screen: the Show and the
Spot. On a Dot, steps 1, 5 and 7 apply.

In the examples the device is named `office`. Use your own device's name: the actions are
`esphome.<name>_...` and the entities `switch.<name>_...` and so on.

## 1. Give the device access to Home Assistant

Do this first. Photos, cameras, weather and the radio lists all need it. The device fetches them from
Home Assistant itself, so it needs a token of its own. The ESPHome connection Home Assistant uses to
talk to the device is not enough for this.

1. In Home Assistant, open your profile (bottom left), then **Security**, and under **Long-lived
   access tokens** create one. Copy it; it is shown only once.
2. In **Developer Tools → Actions**, switch to YAML mode and run:

   ```yaml
   action: esphome.office_home_assistant
   data:
     url: "http://homeassistant.local:8123"
     token: "paste the token here"
   ```

Use an address the device can reach on your network: `homeassistant.local` or Home Assistant's
local IP address. An external or Nabu Casa address does not work.

If a photo folder says **Couldn't open this folder**, or a camera shows `hass: no access configured`,
this step is missing or the address is wrong.

Also turn on **Allow the device to perform Home Assistant actions** on the device's ESPHome entry
(Configure), as in [Getting started](getting-started.md#after-installing-every-device).

## 2. Photos

The slideshow shows photos from Home Assistant's media library. Nothing is stored on the device.

1. **Put the photos where Home Assistant's Media page can see them.** The simplest is a folder in
   Home Assistant's media folder:
   - The **Samba share** add-on shows it as its own share called `media`.
   - The **Terminal & SSH** and **Studio Code Server** add-ons show it as `/media`.
   - Make a folder there, `photos` for example, and copy the pictures in.

   The **File editor** add-on only sees the config folder. A `media` folder made there is not
   Home Assistant's media folder, and the photos will not show up.

   Anything else the Media page can browse works too, such as an Immich album or a network share.
2. **Check** that the photos show in Home Assistant under **Media → My media**. If Home Assistant
   can see them, the device can too.
3. **On the device**, swipe down for Settings, then **Display**:
   - **Slideshow**: **Background** puts the photos behind the clock. **Screensaver** takes over the
     whole screen after a while with nothing happening.
   - **Photo folder**: pick the folder, then **Use this folder**.
   - **Time per photo**, **Shuffle photos** and **Include subfolders** are optional.

The same settings are entities in Home Assistant, and the folder can be set with the
[`home_slideshow` action](actions.md#set-the-slideshows-photo-source).

## 3. Weather

With access set up (step 1), the clock shows Home Assistant's own forecast for your home with no
setup at all. To use a different `weather.*` entity, or none, use
[`home_weather`](actions.md#choose-the-weather-shown-on-the-idle-screen). Settings → Display also
has **Weather animation**, **Radar source** and **Weather alerts** (alerts are U.S. only, from the
National Weather Service).

## 4. Cameras

With access set up, the Cameras drawer (swipe in from the right edge) lists every camera Home
Assistant has. To show only some, with friendlier names, use
[`home_cameras`](actions.md#choose-which-cameras-the-device-shows).

To show a camera when something happens, like the doorbell, call
[`home_show_camera`](actions.md#show-a-camera-on-screen) from an automation:

```yaml
action: esphome.office_home_show_camera
data:
  entity: camera.front_door
  seconds: 60
```

## 5. Music

- **Music Assistant.** Each device is a Sendspin player, on from the first boot. Music Assistant
  finds it on the network with nothing to set up. Several devices can play in sync as a group.
  Read [what this trusts](getting-started.md#after-installing-every-device) first if your Wi-Fi has
  guests on it. **Music Assistant player** under Settings → Sound turns it off.
- **Radio.** The Radio drawer and its favorites are set with
  [the radio actions](actions.md#wire-up-the-radio-page). While a station plays, the **Radio station**,
  **Radio artist** and **Radio title** sensors say what's on (the artist and title when the station's
  service reports them), for an automation or a dashboard to use.

## 6. Night and the screen

All of these are on the Show, under Settings → **Display**, and are entities in Home Assistant.

- **Night hours**: when the screen dims by itself. **At night** picks dark, a faint glow, or a clock
  alone. During the night only a call wakes the screen.
- **Night mode** switch in Home Assistant: start or end the night now, from a bedtime automation for
  example. Set Night hours to **Controlled by Home Assistant** to leave the night to the switch alone.
  See [Turning the night on from an automation](actions.md#turning-the-night-on-from-an-automation).
- **Clock format**, **Clock position** (center, or a smaller clock in a bottom corner so a photo
  stays in view) and **Date color**.
- **Theme**, **Answer time** and **Now playing** (the full page, or a strip over the clock).
- **Turn screen**: how a voice request looks. **Classic** is the words, **Wave** is glowing lines and
  **Bars** is an LED-style equalizer, both moving with the voice. This one is on the Spot too.

## 7. Voice

Under Settings → **Sound**, or on the device's Assist satellite in Home Assistant:

- **Wake word**: Okay Nabu, Hey Jarvis, Alexa and others. Choosing none is fine if you only want
  the screen and music.
- **Wake word sensitivity**: raise it if the device wakes by mistake.
- **Quiet hours** and **Do not disturb**.

Alarms and timers work by voice, on the screen, and from Home Assistant. See
[docs/actions.md](actions.md) for all of them.

## More

- [Dashboards](dashboards.md): Home Assistant dashboards on the screen.
- [Phone calls](phone.md) through your own SIP provider.
- [Actions](actions.md): everything Home Assistant can ask the device to do, with examples.

## Questions and problems

Open an [issue on GitHub](https://github.com/HuskerMinion/techo5/issues). Say which device and
version (Settings → General → Updates shows it), what you did and what happened. Answers there help the next person
too.
