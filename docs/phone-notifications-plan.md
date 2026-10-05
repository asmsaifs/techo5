# Phone notifications on the Show: plan

A phone's notifications, on the Show's screen: who wrote and in which app, and, if you choose, what they
said. The first version gets them through Home Assistant rather than from TECHO5 Cast, so nothing new
runs on the phone and nothing new is opened on the Show.

## How they get here

The Home Assistant Android app already has a **Last notification** sensor
(`sensor.<phone>_last_notification`) and a **Last removed notification** sensor
(`sensor.<phone>_last_removed_notification`). Each notification an allowed app posts becomes the
sensor's new value:

- the state: the notification's text, or the app's package when it has none (cut to 255 characters);
- attributes: `android.title`, `android.text`, `android.textLines`, `package`, `post_time`
  (milliseconds), `is_ongoing`, `is_clearable`, `category` and the rest of the notification's extras.

The Show already follows Home Assistant entities over the ESPHome connection (`hastate`, which is how
the weather and the glance strip work). It follows these too, so no automation is needed: name the
sensors once and the device keeps watching them.

```yaml
action: esphome.office_phone_notifications
data:
  entities: sensor.pixel_8_last_notification
```

## Which ones come through

The phone decides first. The sensor's **Allow list**, in the companion app (Settings → Companion app →
Manage sensors → Last notification), chooses the apps. **Leave it empty and every app's notifications
go to Home Assistant**, so set it: a messaging app or two, the doorbell's app, not the bank.

Then the Show skips:

- ongoing ones (`is_ongoing`): music, navigation, a download, a call in progress;
- ones with neither title nor text;
- old ones: a notification is shown only if it was posted in the last two minutes. That is what keeps a
  restart, or Home Assistant reconnecting, from bringing back the last one again;
- the one already shown.

A summary stands in for its message. WhatsApp posts the chat's notification and then a group summary
(title "WhatsApp", text "14 messages from 5 chats", style `InboxStyle`) a moment apart, and the
companion app sends Home Assistant only the second. The summary's `android.textLines` holds its
lines, newest last, joined with ", " ("Sanji: Hello, ~ Rijvi @ Family: …, Sanji: Hey"). When a
notification's title is just its app's name and it has lines, the card is the last line, read back
from the end to the last piece that starts with a sender ("Name: "), since a message can hold ", "
itself. Lines are used only if they arrived with this notification: Home Assistant does not send an
attribute a notification lacks, so lines that did not come with it are the last summary's.

## What the screen shows

A card in the middle of the screen, the size of an event's pop-up:

- heading: the app and the phone, `WHATSAPP · PIXEL 8`, and **TAP TO DISMISS**;
- the title (for a message, the sender);
- the text, up to two lines, **only with "Phone notifications: show text" on**. It is off by default: the
  screen belongs to the room, and a message, a login code or a bank alert is not the room's business.

It goes away after two minutes, at a tap, or when the notification is dismissed on the phone (the Last
removed notification sensor names the same package and post time). A newer notification replaces the
one up. Below an event pop-up, a reminder and the activation code in importance, so those cover it.

By day it lights a dark screen, the way an event pop-up does. At night it does not; it is there when
the screen is next woken, if it hasn't timed out. It makes no sound: the phone has already made one.

The Show only. The Spot's round face and the Dot have no room for it.

## In Home Assistant

- action `phone_notifications` (`entities`: Last notification sensors, comma separated, up to 4; empty
  turns it off). The removed sensor is found from each name.
- switch **Phone notifications: show text** (configuration).

## What testing on a Show found

- With the Allow list empty, a watch's companion app posting an ongoing notification every few seconds
  kept the sensor to itself: a message got through only between two of its updates. The Allow list is
  not optional.
- The Last notification sensor has no attributes until an allowed app posts something, so a Show
  that follows a fresh sensor sees a state and nothing else.
- WhatsApp's chat notification never reaches Home Assistant, only the summary after it (above).
- The log says why a notification was not shown (its package, age, and whether it had a title), and
  never its words. Ongoing ones are not logged: some apps post one every few seconds.

## Not in this version

- Replying, or the notification's own buttons.
- Dismissing on the phone from the Show.
- A sound, or quiet-hours rules beyond "not at night".
- Getting them straight from TECHO5 Cast, without Home Assistant. That is the second version, if this
  one earns it: it would add dismissing both ways and notifications that arrive with Home Assistant
  down.

## Pieces

| Where | What |
|---|---|
| `config/notifications.go` | `Notifications{Entities, ShowText}` |
| `feature/notification` | follows the sensors, decides what is new, holds the card, the action and the switch |
| `feature/display/render_notification.go` | the card |
| `feature/display/display.go` | lighting the screen, the tap, the redraw |
| `docs/actions.md` | the action and the switch |
