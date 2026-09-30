//go:build spot

package all

// notOnThisDevice: the Echo Spot build has every entity the Show's has, but the calendar pop-ups, the night
// screen settings and Cast (its receiver is the Show's).
var notOnThisDevice = []string{"cast", "cast_ask", "cast_state", "calendar_popup_all_day", "calendar_popup_before", "calendar_popup_chime", "calendar_popups", "screen_now_playing", "screen_theme", "screen_at_night", "screen_night_clock_style", "screen_night_end", "screen_night_hours", "screen_night_light_level", "screen_clock_position", "screen_date_color", "screen_night_mode", "screen_night_start"}
