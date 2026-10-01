package setup

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/streaming"
)

// streamingSection is AirPlay and Spotify Connect (feature/streaming): the device as a speaker an
// iPhone or the Spotify app plays to, under the device's own name.
func streamingSection(w http.ResponseWriter, token string) {
	if !streaming.Here {
		return
	}
	c := config.Get().Streaming
	checked := func(on bool) string {
		if on {
			return " checked"
		}
		return ""
	}
	fmt.Fprint(w, `<fieldset><legend>AirPlay and Spotify Connect</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "streaming", "sound")
	fmt.Fprintf(w, `<p class="note" style="margin-top:0">Play to this device from other apps. It shows up under its
	  own name, on the same network.</p>
	 <p><label><input type="checkbox" name="airplay" value="yes" style="width:auto"%s> AirPlay: from an iPhone, iPad or Mac</label></p>
	 <p><label><input type="checkbox" name="spotify" value="yes" style="width:auto"%s> Spotify Connect: from the Spotify app (needs Spotify Premium)</label></p>
	 <p class="note">Anyone on the same network can play to it while one is on, as with any AirPlay or Spotify
	  speaker. Spotify Connect keeps the login a phone hands it until Spotify Connect is turned off.</p>
	 <p class="note">New, and not yet tried with an iPhone or a Spotify account: if something does not work, say so on
	  GitHub.</p>
	 <p><button type="submit">Save</button></p></form></fieldset>`, checked(c.AirPlay), checked(c.Spotify))
}

func saveStreaming(r *http.Request) string {
	airplay, spotify := r.PostFormValue("airplay") == "yes", r.PostFormValue("spotify") == "yes"
	if airplay != config.Get().Streaming.AirPlay {
		streaming.Get().SetAirPlay(airplay)
	}
	if spotify != config.Get().Streaming.Spotify {
		streaming.Get().SetSpotify(spotify)
	}
	if config.Get().Streaming.AirPlay != airplay || config.Get().Streaming.Spotify != spotify {
		return "could not save it"
	}
	slog.Info("setup page: streaming set", "airplay", airplay, "spotify", spotify)
	return ""
}
