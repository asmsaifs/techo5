package setup

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/metrics"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/musicassistant"
)

// musicSection is the Music Assistant server the voice assistant asks for songs, artists, albums and
// playlists (config.MusicAssistant). The token is never shown: it is written, or left alone.
func musicSection(ctx context.Context, w http.ResponseWriter, token string) {
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
	if m.Set() {
		fmt.Fprintf(w, `<p style="margin-top:0"><b>Now:</b> %s</p>`, html.EscapeString(musicStatus(ctx, m)))
	}
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

// musicStatus is how the music library sees this device right now: whether it answers, and whether
// this device is connected to it as a player, which is what everything it plays here needs.
func musicStatus(ctx context.Context, m config.MusicAssistant) string {
	player, err := layout.FactoryMAC()
	if err != nil {
		return "this device does not know its own player id"
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	st, err := musicassistant.Client{URL: m.URL, Token: m.Token}.Player(ctx, player)
	switch {
	case err != nil && strings.Contains(err.Error(), "refused the token"):
		return "reachable, but it refused the token: make a new one and save it here"
	case err != nil && strings.Contains(err.Error(), "did not answer"):
		return "not reachable: " + err.Error()
	case err != nil:
		return err.Error()
	case !st.Available:
		return "reachable, but this device is not connected to it as a player, so it cannot play from it"
	case st.State == "playing":
		return "connected, and playing on this device"
	}
	return "connected: this device is a player in it"
}
