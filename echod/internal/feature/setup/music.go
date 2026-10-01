package setup

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/metrics"
)

// musicSection is the Music Assistant server the voice assistant asks for songs, artists, albums and
// playlists (config.MusicAssistant). The token is never shown: it is written, or left alone.
func musicSection(w http.ResponseWriter, token string) {
	m := config.Get().MusicAssistant
	held := "a long-lived token"
	if m.Token != "" {
		held = "saved: leave empty to keep it"
	}
	var addrs []string
	for _, ip := range metrics.Addresses() {
		addrs = append(addrs, ip.String())
	}
	fmt.Fprint(w, `<fieldset><legend>Music Assistant</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "music", "sound")
	fmt.Fprintf(w, `<p class="note" style="margin-top:0">For "play some Eagles" by voice, when the voice assistant
	  answers directly: it searches this Music Assistant and plays what it finds here. Music Assistant has to
	  know this device as a player first. On the same network it finds it by itself; from farther away, add
	  one of this device's addresses (%s) in Music Assistant's Sendspin settings, under the manual discovery
	  IP addresses. Make the token for a Music Assistant user allowed only this device's player.</p>
	 <label for="maurl">Address</label>
	 <input id="maurl" name="url" value="%s" placeholder="http://192.168.1.20:8095" autocomplete="off">
	 <label for="matoken">Token</label>
	 <input id="matoken" name="matoken" type="password" value="" placeholder="%s" autocomplete="off">
	 <p><label><input type="checkbox" name="notoken" value="yes" style="width:auto"> Remove the token</label></p>
	 <p><button type="submit">Save</button></p></form></fieldset>`,
		html.EscapeString(strings.Join(addrs, ", ")), html.EscapeString(m.URL), html.EscapeString(held))
}

func saveMusic(r *http.Request) string {
	addr := strings.TrimRight(strings.TrimSpace(r.PostFormValue("url")), "/")
	if addr != "" {
		u, err := url.Parse(addr)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "the address should be like http://192.168.1.20:8095"
		}
		if u.User != nil {
			return "the address should not have a name or password in it: the token is what signs in"
		}
	}
	var tok *string
	// Not "token": that is the page's own, which every form carries (hidden).
	switch t := strings.TrimSpace(r.PostFormValue("matoken")); {
	case r.PostFormValue("notoken") == "yes":
		empty := ""
		tok = &empty
	case t != "":
		if strings.ContainsFunc(t, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) {
			return "a token is one word"
		}
		tok = &t
	}
	if err := config.Set().MusicAssistant().Server(addr, tok); err != nil {
		return "could not save it: " + err.Error()
	}
	slog.Info("setup page: music assistant set", "url", addr, "token", tok != nil)
	return ""
}
