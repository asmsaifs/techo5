package home

import esphome "github.com/ygelfand/go-esphome-device"

// What the radio is playing, as Home Assistant sensors: the station, and the artist and title when the
// station's service says what is on. The device's media player cannot carry them, since ESPHome's
// media player state has no such fields, so an automation or a Music Assistant plugin reads them here.
// Each is empty while no radio plays, and the artist and title while the station says nothing.

func (f *Feature) buildRadioSensors() {
	text := func(id, name, icon string) *esphome.TextSensor {
		return &esphome.TextSensor{Base: esphome.Base{ObjectID: id, Name: name, Icon: icon}}
	}
	f.radioStationTxt = text("radio_station", "Radio station", "mdi:radio")
	f.radioArtistTxt = text("radio_artist", "Radio artist", "mdi:account-music")
	f.radioTitleTxt = text("radio_title", "Radio title", "mdi:music-note")
}

// showRadio puts m on the radio sensors.
func (f *Feature) showRadio(m meta) {
	f.radioStationTxt.Set(m.station)
	f.radioArtistTxt.Set(m.now.Artist)
	f.radioTitleTxt.Set(m.now.Title)
}
