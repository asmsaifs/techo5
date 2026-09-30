package setup

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// reolinkSection is a Reolink NVR, Home Hub or camera read directly (feature/home/reolink.go). The
// password is never shown: it is written, or left alone.
func reolinkSection(w http.ResponseWriter, token string) {
	r := config.Get().Home.Reolink
	fmt.Fprint(w, `<fieldset><legend>Reolink cameras</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "reolink", "connections")
	if r.Base != "" {
		var names []string
		for _, c := range r.Cameras {
			names = append(names, html.EscapeString(c.Name))
		}
		fmt.Fprintf(w, `<p style="margin:0">Set up: %d camera(s): %s. Say "show the" and a camera's name, or
		 swipe in from the right edge.</p>`, len(r.Cameras), strings.Join(names, ", "))
	}
	host := strings.TrimPrefix(strings.TrimPrefix(r.Base, "https://"), "http://")
	hint := "the recorder's password"
	if r.Pass != "" {
		hint = "set; leave empty to keep it"
	}
	fmt.Fprintf(w, `<label for="rlhost">NVR, Home Hub or camera address</label>
	 <input id="rlhost" name="host" value="%s" placeholder="192.168.1.30" autocomplete="off">
	 <label for="rluser">User</label>
	 <input id="rluser" name="user" value="%s" placeholder="admin" autocomplete="off">
	 <label for="rlpass">Password</label>
	 <input id="rlpass" name="pass" type="password" value="" placeholder="%s" autocomplete="off">
	 <p class="note">The device logs in to the recorder and lists its cameras by the names they have in the
	  Reolink app. A user of its own with only "view" rights is the tidy way. Clear the address and save
	  to take the cameras away.</p>
	 <p><button type="submit">Save</button></p></form></fieldset>`,
		html.EscapeString(host), html.EscapeString(r.User), hint)
}

func saveReolink(r *http.Request) string {
	host := strings.TrimSpace(r.PostFormValue("host"))
	user := strings.TrimSpace(r.PostFormValue("user"))
	if strings.ContainsAny(host+user+r.PostFormValue("pass"), "\r\n") {
		return "the address, user and password are one line each"
	}
	if host != "" && user == "" {
		user = "admin"
	}
	n, err := home.SetReolink(host, user, r.PostFormValue("pass"))
	if err != nil {
		return "could not set up the cameras: " + err.Error()
	}
	slog.Info("setup page: Reolink cameras set", "cameras", n)
	return ""
}
