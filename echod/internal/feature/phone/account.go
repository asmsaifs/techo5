package phone

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

// accountPath holds the SIP login, in its own file, like the API key, owner-only as state.json is.
var accountPath = filepath.Join(layout.StateDir, "phone.json")

// Account is one SIP login at a provider.
type Account struct {
	// Server is the provider's host, e.g. a VoIP.ms POP, with an optional port
	// (sip.linphone.org:443) for a network that blocks the usual SIP ports.
	Server string `json:"server"`

	Username string `json:"username"`
	Password string `json:"password"`

	// Plain turns TLS and SRTP off, for a provider or network that cannot do them. Off by default: a
	// call is someone's voice in their home.
	Plain bool `json:"plain,omitempty"`
}

func (a Account) valid() bool { return a.Server != "" && a.Username != "" && a.Password != "" }

// hostPort splits Server into its host and port; with no port given, it is def.
func (a Account) hostPort(def int) (string, int) {
	h, p, err := net.SplitHostPort(a.Server)
	if err != nil {
		return a.Server, def
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return a.Server, def
	}
	return h, n
}

func loadAccount() (Account, error) {
	var a Account
	b, err := os.ReadFile(accountPath)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	err = json.Unmarshal(b, &a)
	return a, err
}

// saveAccount replaces the login, or removes it for an empty one. Owner-only, through a temporary file
// so a crash leaves the old one or the new one.
func saveAccount(a Account) error {
	a.Server = strings.TrimSpace(a.Server)
	a.Username = strings.TrimSpace(a.Username)
	if a.Username == "" {
		err := os.Remove(accountPath)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !a.valid() {
		return errors.New("phone: a server, a username and a password are all needed")
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	tmp := accountPath + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, accountPath)
}
