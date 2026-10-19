package display

// watchedLight is the backlight while the camera may be streamed: a dark panel keeps the dimmest light
// while anything watches (feature/camstream), so the dot that says so can be seen in the room. A lit
// panel's own light is left alone.
func watchedLight(target float64, on, watched bool, dimmest float64) float64 {
	if !on && watched && target < dimmest {
		return dimmest
	}
	return target
}
