//go:build spot

package all

// notOnThisDevice: what the Echo Spot build leaves out of the shared list: the LED ring's entities, as
// on the Show, the Show's own screen settings and Cast (its receiver is the Show's).
var notOnThisDevice = append(ringOnly, "cast", "cast_ask", "cast_state", "phone_notifications_sound", "phone_notifications_text", "calendar_popup_all_day", "calendar_popup_before", "calendar_popup_chime", "calendar_popups", "screen_now_playing", "screen_theme", "screen_auto_brightness_dimmest", "screen_date_color", "screen_clock_tap", "screen_dashboard_tiles", "lyrics", "screen_night_tap_lifts")

// deviceSpecific: the audio output, and the subtle muted ring, since only the Spot draws its mute on
// the screen; and the video player (feature/video), which the Show has too and the Dot does not.
var deviceSpecific = []string{"audio_output", "screen_mute_ring_subtle", "video", "dlna_video", "video_state", "video_title", "video_error"}

// actionsNotOnThisDevice: the Spot offers every action the shared code has.
var actionsNotOnThisDevice = map[string]string{}
