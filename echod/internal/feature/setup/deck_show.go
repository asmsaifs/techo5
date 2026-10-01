//go:build !dot

package setup

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/dashboard"
)

// foundDecks is what the last look for deck apps heard, kept for the page that is drawn next. The
// look takes a couple of seconds, so it is a button of its own rather than part of drawing the page.
var foundDecks struct {
	sync.Mutex
	list []dashboard.FoundDeck
}

// deckSection is where the desktop Stream Deck app is: its address and key, and whether it stands in
// for the clock. Finding the app only fills in the address; the key is always typed by hand.
func deckSection(w http.ResponseWriter, token string) {
	d := config.Get().Deck
	keyNote := "No key saved yet."
	if d.Key != "" {
		keyNote = "A key is saved. Leave this empty to keep it."
	}
	foundDecks.Lock()
	found := append([]dashboard.FoundDeck(nil), foundDecks.list...)
	foundDecks.Unlock()

	fmt.Fprint(w, `<fieldset><legend>Stream Deck</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "deck", "connections")
	fmt.Fprintf(w, `<label for="deckaddr">Address</label>
	 <input id="deckaddr" name="address" value="%s" placeholder="192.168.1.20:9555" autocomplete="off" list="decks">
	 <datalist id="decks">`, html.EscapeString(d.Server))
	for _, f := range found {
		fmt.Fprintf(w, `<option value="%s">%s</option>`, html.EscapeString(f.Address), html.EscapeString(f.Name))
	}
	idle := ""
	if d.Idle {
		idle = " checked"
	}
	fmt.Fprintf(w, `</datalist>
	 <label for="deckkey">Key</label>
	 <input id="deckkey" name="key" type="password" autocomplete="off">
	 <p class="note">%s</p>
	 <label><input type="checkbox" name="idle" value="1"%s> Show the deck in place of the clock</label>
	 <p class="note">The desktop Stream Deck app, apart from the dashboard's server. Swipe in from the
	  <strong>right</strong> edge to open it; the drawer is then a swipe in from the right on the deck.
	  Leave the address empty to turn it off.</p>
	 <p><button type="submit">Save</button></p></form>`, html.EscapeString(keyNote), idle)
	fmt.Fprint(w, `<form method="post" action="/setup/save">`)
	hidden(w, token, "deckfind", "connections")
	if len(found) > 0 {
		names := make([]string, 0, len(found))
		for _, f := range found {
			names = append(names, f.Name+" ("+f.Address+")")
		}
		fmt.Fprintf(w, `<p class="note">Found: %s. Pick the address from the list above.</p>`, html.EscapeString(strings.Join(names, ", ")))
	}
	fmt.Fprint(w, `<p><button type="submit">Look for decks</button></p></form></fieldset>`)
}

// saveDeck keeps the deck's server, key and idle choice. A key left empty is kept as it was.
func saveDeck(r *http.Request) string {
	addr := r.PostFormValue("address")
	key := strings.TrimSpace(r.PostFormValue("key"))
	if key == "" {
		key = config.Get().Deck.Key
	}
	if err := dashboard.Get().SetDeck(addr, key); err != nil {
		return "could not save it: " + err.Error()
	}
	if err := config.Set().Deck().Idle(r.PostFormValue("idle") != ""); err != nil {
		return "could not save it: " + err.Error()
	}
	return ""
}

// findDecks listens for deck apps and keeps what it heard for the page.
func findDecks(r *http.Request) string {
	list := dashboard.Discover(r.Context())
	foundDecks.Lock()
	foundDecks.list = list
	foundDecks.Unlock()
	return ""
}
