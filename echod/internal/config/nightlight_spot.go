//go:build spot

package config

// nightLightRescale is the Spot's night clock level saved on its old scale, backlight 6, 10, 14, 20,
// 28, 38, 50, 64, 80, 100, as the step of the new one, 3, 4, 6, 8, 12, 16, 22, 30, 40, 50
// (feature/display's glowSteps), whose backlight is nearest it as the eye sees it: by ratio, not by
// difference. Everything above the new top is the top.
var nightLightRescale = [...]int{3, 5, 6, 7, 8, 9, 10, 10, 10, 10}

// rescaleNightLight puts a night clock level saved on the old scale on the new one: once, marked by
// NightLightRescaled, which a file saved before the new scale does not mention. None set stays none,
// which is the new default.
func (c *Config) rescaleNightLight() {
	if c.Screen.NightLightRescaled {
		return
	}
	if v := c.Screen.NightLightLevel; v > 0 {
		c.Screen.NightLightLevel = nightLightRescale[min(v, len(nightLightRescale))-1]
	}
	c.Screen.NightLightRescaled = true
}
