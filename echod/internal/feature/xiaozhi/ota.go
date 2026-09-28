package xiaozhi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

// The OTA call is how a device finds the server to talk to. It happens on every connection attempt
// and it is the only place the WebSocket url and the token come from: the two are never worked out
// from the host, because that is what would tie a self-hosted server and an official cloud outage
// to a code change.

const (
	// OfficialHost is the cloud a device works against out of the box, needing nothing but the
	// network. It is a default rather than a constant, because the host is a setting.
	OfficialHost = "api.tenclass.net"

	// OTAPath is where the endpoint lives. The official cloud and xinnan-tech/xiaozhi-esp32-server
	// both serve it here, and both return /xiaozhi/v1/ for the WebSocket, which is what makes one
	// code path cover both.
	OTAPath = "/xiaozhi/ota/"

	// otaTimeout is one call's budget. It is a small JSON document from somewhere in China; if it
	// has not arrived in twenty seconds the network has a problem worth reporting rather than
	// retrying silently.
	otaTimeout = 20 * time.Second

	// otaUserAgent says what is asking. The endpoint does not check it and neither does anything
	// else, but a server log that cannot say who asked is a server log nobody reads.
	otaUserAgent = "techo5-xiaozhi/1"
)

// otaURL is the endpoint to ask, from whatever host is configured.
//
// A bare name is the cloud and the cloud is https. A scheme in the setting is taken at its word,
// because the other server this client is meant to reach — a self-hosted xiaozhi-esp32-server — is
// usually a box on the same network with no certificate for it, and a setting that insists on https
// cannot talk to one at all.
func otaURL(host string) string {
	host = strings.TrimSpace(host)
	switch {
	case host == "":
		return "https://" + OfficialHost + OTAPath
	case !strings.Contains(host, "://"):
		host = "https://" + host
	}
	return strings.TrimSuffix(host, "/") + OTAPath
}

// identity is what the device calls itself, in the two forms the endpoint insists on.
//
// Both formats are load-bearing and neither error says which one is wrong: the OTA endpoint answers
// a bare `400` to a MAC written without colons ("Invalid MAC address") and to a client id written
// without dashes ("Invalid client ID"). Both were found by sending the wrong shape and reading the
// server's log, so they are written down here rather than discovered again.
type identity struct {
	// DeviceID is the factory MAC, with colons.
	DeviceID string

	// ClientID is a UUID v4, with dashes, generated once and kept: it is the device's identity to
	// the cloud, and a device that loses it is a device that has to be activated again.
	ClientID string
}

// factoryMAC is where the device's own address comes from. A variable so a test can supply one: the
// real one reads /proc/idme, which is a kernel interface on the device and nowhere else.
var factoryMAC = layout.FactoryMAC

// identify works out who this device is, writing a client id the first time it is asked.
//
// Generating it here rather than at install is deliberate. A device that has never spoken to the
// cloud should not carry an identity in its settings file, and a device that has should not lose it
// because the file was replaced.
func identify(ctx context.Context) (identity, error) {
	mac, err := factoryMAC()
	if err != nil {
		return identity{}, err
	}

	id := identity{DeviceID: mac, ClientID: config.Get().Xiaozhi.ClientID}
	if id.ClientID != "" {
		return id, nil
	}

	id.ClientID, err = newUUIDv4()
	if err != nil {
		return identity{}, err
	}
	if err := config.Set().Xiaozhi().ClientID(id.ClientID); err != nil {
		return identity{}, fmt.Errorf("keeping the xiaozhi client id: %w", err)
	}
	slog.Info("xiaozhi: this device's cloud identity", "client_id", id.ClientID)
	return id, nil
}

// ota is what the endpoint answers.
type ota struct {
	Websocket struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	} `json:"websocket"`

	// Activation comes back as an object with a code in it while the device is unactivated, and as
	// null once it has been redeemed. A server that does not do activation leaves it out.
	Activation json.RawMessage `json:"activation"`

	ServerTime struct {
		Timestamp int64 `json:"timestamp"`
		Offset    int   `json:"timezone_offset"`
	} `json:"server_time"`
}

// activation is the code a device shows its owner, and the challenge behind it.
type activation struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Challenge is what a QR on the settings page would carry; the code alone is what the owner
	// types at xiaozhi.me.
	Challenge string `json:"challenge"`
}

// code returns the activation still to be redeemed, and whether there is one.
//
// The official cloud does not close the WebSocket on an unactivated device: it completes the
// upgrade, accepts the hello, and then closes with 1002 a moment later. So a device has to be
// activated before it dials, and the only way to know is the code still coming back here.
func (o *ota) code() (activation, bool) {
	if len(o.Activation) == 0 || string(o.Activation) == "null" {
		return activation{}, false
	}
	var a activation
	if err := json.Unmarshal(o.Activation, &a); err != nil {
		// Something is there and it is not a code. That is still an unactivated device, and showing
		// nothing would leave it waiting for a reason it cannot name.
		slog.Warn("xiaozhi: the activation in the OTA response is not readable", "raw", string(o.Activation))
		return activation{}, true
	}
	return a, a.Code != ""
}

// fetchOTA asks the endpoint where to connect and whether this device is activated.
//
// The url is passed in rather than read from the settings here, so that the one place that decides
// which endpoint to talk to is the caller and there is only one of it.
func fetchOTA(ctx context.Context, url string, id identity) (*ota, error) {
	body, err := json.Marshal(map[string]any{
		"version":     1,
		"language":    "zh-CN",
		"mac_address": id.DeviceID,
		"uuid":        id.ClientID,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Activation-Version", "1")
	req.Header.Set("Device-Id", id.DeviceID)
	req.Header.Set("Client-Id", id.ClientID)
	req.Header.Set("User-Agent", otaUserAgent)
	req.Header.Set("Accept-Language", "zh-CN")

	client := &http.Client{Timeout: otaTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the OTA response: %w", err)
	}
	var out ota
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the OTA response from %s is not the shape it should be: %w", url, err)
	}
	if out.Websocket.URL == "" {
		// A 200 with nothing in it means the endpoint is not one of these, which is a different
		// problem from being down and worth saying so rather than dialling an empty string.
		return nil, fmt.Errorf("%s answered without a websocket url", url)
	}
	return &out, nil
}

// newUUIDv4 builds a random RFC 4122 version 4 UUID, in the dashed form the endpoint requires.
//
// Hand-rolled rather than pulled in: it is eight lines, it is the only UUID this program needs, and
// a dependency for it would be a licence note and a supply chain for something the standard library
// can do.
func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("entropy: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
