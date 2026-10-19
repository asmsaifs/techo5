package config

import "testing"

func TestWeatherEntity(t *testing.T) {
	for _, c := range []struct{ saved, want string }{
		{"", DefaultWeather},
		{WeatherOff, ""},
		{"weather.backyard", "weather.backyard"},
	} {
		if got := (Home{Weather: c.saved}).WeatherEntity(); got != c.want {
			t.Errorf("WeatherEntity(%q) = %q, want %q", c.saved, got, c.want)
		}
	}
	// Home Assistant listed its weather entities: the default only while it has its own forecast.
	if got := (Home{WeatherSources: []string{"weather.a", DefaultWeather}}).WeatherEntity(); got != DefaultWeather {
		t.Errorf("with its own forecast listed: %q", got)
	}
	if got := (Home{WeatherSources: []string{"weather.a", "weather.b"}}).WeatherEntity(); got != "weather.a" {
		t.Errorf("without its own forecast: %q, want the first listed", got)
	}
	if got := (Home{Weather: WeatherOff, WeatherSources: []string{"weather.a"}}).WeatherEntity(); got != "" {
		t.Errorf("none chosen: %q", got)
	}
}

// Wiring the radio page leaves the device's own stations alone: they are typed on the setup page,
// and a home_radio call, which says nothing about them, is no reason to lose them.
func TestWiringTheRadioKeepsTheDevicesOwnStations(t *testing.T) {
	st := load(t)
	own := []Station{{Name: "Jazz", URL: "https://stream.example/jazz"}}
	if err := st.Set().Home().OwnStations(own); err != nil {
		t.Fatal(err)
	}
	if err := st.Set().Home().Radio(Radio{Stations: []string{"input_select.radio"}, Service: "script.play_radio"}); err != nil {
		t.Fatal(err)
	}
	r := st.Get().Home.Radio
	if len(r.Own) != 1 || r.Own[0] != own[0] {
		t.Errorf("own stations after wiring: %+v, want %+v", r.Own, own)
	}
	if r.Service != "script.play_radio" || len(r.Stations) != 1 {
		t.Errorf("the wiring was not saved: %+v", r)
	}
	// And the other way round: changing the own stations keeps the wiring.
	if err := st.Set().Home().OwnStations(nil); err != nil {
		t.Fatal(err)
	}
	if r := st.Get().Home.Radio; len(r.Own) != 0 || r.Service != "script.play_radio" {
		t.Errorf("after clearing the own stations: %+v", r)
	}
}
