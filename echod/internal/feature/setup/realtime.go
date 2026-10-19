package setup

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func init() { saves["realtime"] = saveRealtime }

// realtimeSlots is how many wake word slots can be chosen for it.
const realtimeSlots = 2

var providerNames = map[string]string{"openai": "OpenAI", "xai": "xAI (Grok)", "gemini": "Google (Gemini)"}

// realtimeSection is realtime voice (config.Realtime): which provider, its key and the rest, and which wake
// word slots it answers. Neither the key nor the token is shown: each is written, or left alone. Choosing
// a slot for it is here only: Home Assistant has no switch for it.
func realtimeSection(w http.ResponseWriter, token string) {
	c := config.Get()
	rt := c.Realtime
	fmt.Fprint(w, `<fieldset><legend>Realtime voice</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "realtime", "sound")
	who := "the provider chosen"
	if n, ok := providerNames[rt.Provider]; ok {
		who = n
	}
	fmt.Fprintf(w, `<p class="note"><strong>A wake word chosen for realtime voice sends what you say, as audio, to
	 %s, while you speak, and their model answers out loud. They bill for it, by the amount of audio.</strong>
	 The device keeps to the minutes a day set here, counting all the time a session is open, and ends any one
	 turn after five minutes. Off until a wake word is ticked below.</p>`, html.EscapeString(who))
	fmt.Fprint(w, `<label for="rtp">Provider</label><select id="rtp" name="provider"><option value="">None</option>`)
	for _, p := range config.RealtimeProviders {
		fmt.Fprintf(w, `<option value="%s"%s>%s</option>`, p, selected(rt.Provider == p), html.EscapeString(providerNames[p]))
	}
	fmt.Fprintf(w, `</select>
	 <label for="rtkey">Key</label>
	 <input id="rtkey" name="key" type="password" value="" placeholder="%s" autocomplete="off">
	 <p><label><input type="checkbox" name="nokey" value="yes" style="width:auto"> Remove the key</label></p>
	 <label for="rtend">Relay (optional)</label>
	 <input id="rtend" name="endpoint" value="%s" placeholder="wss://relay.home.arpa/realtime" autocomplete="off">
	 <p class="note">A relay on your network in place of the provider's own address, which can hold the key
	  itself: a wss:// address only, since your voice goes through it.</p>
	 <label for="rtmodel">Model</label>
	 <input id="rtmodel" name="model" value="%s" placeholder="the provider's own" autocomplete="off">
	 <label for="rtvoice">Voice</label>
	 <input id="rtvoice" name="voice" value="%s" placeholder="the provider's own" autocomplete="off">
	 <label for="rtmin">Minutes a day, at most</label>
	 <input id="rtmin" name="minutes" type="number" min="1" max="%d" value="%d">
	 <p class="note">Used today: %d of %d minutes.</p>
	 <label for="rtmcp">Home Assistant token for its MCP server (optional)</label>
	 <input id="rtmcp" name="mcp" type="password" value="" placeholder="%s" autocomplete="off">
	 <p><label><input type="checkbox" name="nomcp" value="yes" style="width:auto"> Remove the token</label></p>
	 <p class="note">With Home Assistant's Model Context Protocol Server integration added, and a long-lived
	  token from a Home Assistant user of its own with no administrator rights, the model can do in the house
	  what Assist can. Without it, it has this device's own abilities only. Either way, once a turn has read
	  the web or a calendar, nothing that changes anything runs in it.</p>`,
		keyHint(rt.Key != ""), html.EscapeString(rt.Endpoint), html.EscapeString(rt.Model), html.EscapeString(rt.Voice),
		config.MaxDailyMinutes, rt.Daily(), usedMinutes(rt), rt.Daily(), keyHint(rt.MCPToken != ""))
	fmt.Fprint(w, `<p>Answer with realtime voice:</p>`)
	for _, i := range realtimeChoices(c) {
		on, name := false, fmt.Sprintf("wake word %d", i+1)
		if i < len(c.Wake.Words) {
			on = c.Wake.Words[i].Realtime
			if id := c.Wake.Words[i].ID; id != "" {
				name = fmt.Sprintf("wake word %d (%s)", i+1, id)
			}
		}
		fmt.Fprintf(w, `<label><input type="checkbox" name="slot%d" value="yes" style="width:auto"%s> %s</label><br>`,
			i, checkedIf(on), html.EscapeString(name))
	}
	fmt.Fprint(w, `<p><button type="submit">Save</button></p></form></fieldset>`)
}

// realtimeChoices is the slots there are to choose: those with a wake word, the first always. Ticking
// one with no word would choose a slot nothing wakes.
func realtimeChoices(c config.Config) []int {
	slots := []int{0}
	for i := 1; i < min(realtimeSlots, len(c.Wake.Words)); i++ {
		if c.Wake.Words[i].ID != "" {
			slots = append(slots, i)
		}
	}
	return slots
}

func usedMinutes(rt config.Realtime) int {
	return (rt.Daily()*60 - rt.SecondsLeft(time.Now()) + 59) / 60
}

// saveRealtime keeps the form, refusing what could not work.
func saveRealtime(r *http.Request) string {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	provider, endpoint := v("provider"), v("endpoint")
	if provider != "" && !slices.Contains(config.RealtimeProviders, provider) {
		return "that is not a provider realtime voice speaks"
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil {
			return "the relay should be a secure address like wss://relay.home.arpa/realtime, with no login in it"
		}
	}
	minutes, err := strconv.Atoi(v("minutes"))
	if err != nil || minutes < 1 || minutes > config.MaxDailyMinutes {
		return fmt.Sprintf("minutes a day are 1 to %d", config.MaxDailyMinutes)
	}
	if strings.ContainsAny(v("model")+v("voice")+r.PostFormValue("key")+r.PostFormValue("mcp"), "\r\n") {
		return "the model, voice, key and token are one line each"
	}
	if err := config.Set().Realtime().Set(provider, endpoint, v("model"), v("voice"), minutes); err != nil {
		return "could not save it: " + err.Error()
	}
	switch key := v("key"); {
	case r.PostFormValue("nokey") == "yes":
		_ = config.Set().Realtime().SetKey("")
	case key != "":
		_ = config.Set().Realtime().SetKey(key)
	}
	switch tok := v("mcp"); {
	case r.PostFormValue("nomcp") == "yes":
		_ = config.Set().Realtime().SetMCPToken("")
	case tok != "":
		_ = config.Set().Realtime().SetMCPToken(tok)
	}
	var chosen []int
	for _, i := range realtimeChoices(config.Get()) {
		on := r.PostFormValue(fmt.Sprintf("slot%d", i)) == "yes"
		was := i < len(config.Get().Wake.Words) && config.Get().Wake.Words[i].Realtime
		if on != was {
			if err := config.Set().Wake(i).Realtime(on); err != nil {
				return "could not save it: " + err.Error()
			}
		}
		if on {
			chosen = append(chosen, i+1)
		}
	}
	if len(chosen) > 0 && !config.Get().Realtime.Ready() {
		return "saved; realtime voice is not set up yet, so a wake word chosen for it says so when it is used"
	}
	slog.Info("setup page: realtime voice saved", "provider", provider, "slots", chosen)
	return ""
}
