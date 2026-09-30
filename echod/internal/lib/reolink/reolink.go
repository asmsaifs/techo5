// Package reolink reads pictures from a Reolink NVR, Home Hub or camera directly, over its HTTP API
// (the api.cgi one the Reolink apps use): a list of its channels by name, and a JPEG snapshot of one.
// For a device with no Home Assistant to proxy the cameras through.
//
// Newer Reolink firmware answers only over HTTPS, with a certificate it made itself, which nothing
// can check. So the certificate is pinned: its fingerprint is taken when the recorder is set up
// (Probe) and every connection after must present the same one.
package reolink

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Channel is one camera on the recorder.
type Channel struct {
	Number int
	Name   string
	Online bool
}

// Client is one recorder.
type Client struct {
	Base        string // https://192.168.1.30 or http://...
	User, Pass  string
	Fingerprint string // the pinned certificate's sha256, hex; empty over plain http

	once   sync.Once
	http   *http.Client
	mu     sync.Mutex
	token  string
	expiry time.Time
}

// maxSnap bounds one picture: a 4K snapshot is a few megabytes.
const maxSnap = 16 << 20

func (c *Client) client() *http.Client {
	c.once.Do(func() {
		tr := &http.Transport{TLSClientConfig: &tls.Config{
			// Checked below against the pinned fingerprint instead: the recorder's certificate is its
			// own, and there is no authority to check it with.
			InsecureSkipVerify: true, //nolint:gosec
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return errors.New("reolink: no certificate")
				}
				if c.Fingerprint != "" && fingerprint(cs.PeerCertificates[0]) != c.Fingerprint {
					return errors.New("reolink: the recorder's certificate is not the one it had when it was set up")
				}
				return nil
			},
		}}
		c.http = &http.Client{Timeout: 15 * time.Second, Transport: tr}
	})
	return c.http
}

func fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// Probe finds how a recorder at host answers - https first, then http - and logs in to check the user
// and password, returning a client with its certificate pinned.
func Probe(ctx context.Context, host, user, pass string) (*Client, error) {
	host = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://"), "/"))
	if host == "" {
		return nil, errors.New("reolink: no address")
	}
	var last error
	for _, scheme := range []string{"https", "http"} {
		c := &Client{Base: scheme + "://" + host, User: user, Pass: pass}
		if scheme == "https" {
			fp, err := peerFingerprint(ctx, host)
			if err != nil {
				last = err
				continue
			}
			c.Fingerprint = fp
		}
		if err := c.login(ctx); err != nil {
			last = err
			if errors.Is(err, errLogin) {
				return nil, err // it answered: the name or password is wrong
			}
			continue
		}
		return c, nil
	}
	return nil, fmt.Errorf("reolink: %s did not answer as a Reolink recorder: %w", host, last)
}

func peerFingerprint(ctx context.Context, host string) (string, error) {
	addr := host
	if !strings.Contains(addr, ":") {
		addr += ":443"
	}
	d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // pinned, see Probe
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("reolink: no certificate")
	}
	return fingerprint(certs[0]), nil
}

var errLogin = errors.New("reolink: the user name or password was not accepted")

type reply struct {
	Cmd   string          `json:"cmd"`
	Code  int             `json:"code"`
	Value json.RawMessage `json:"value"`
	Error *struct {
		Detail  string `json:"detail"`
		RspCode int    `json:"rspCode"`
	} `json:"error"`
}

func (c *Client) post(ctx context.Context, cmd, token string, param any) (reply, error) {
	body, _ := json.Marshal([]map[string]any{{"cmd": cmd, "action": 0, "param": param}})
	u := c.Base + "/cgi-bin/api.cgi?cmd=" + cmd
	if token != "" {
		u += "&token=" + token
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return reply{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return reply{}, err
	}
	var rs []reply
	if err := json.Unmarshal(b, &rs); err != nil || len(rs) == 0 {
		return reply{}, fmt.Errorf("reolink: %s: not an API answer", cmd)
	}
	r := rs[0]
	if r.Code != 0 || r.Error != nil {
		detail := ""
		if r.Error != nil {
			detail = r.Error.Detail
		}
		return r, fmt.Errorf("reolink: %s: %s", cmd, detail)
	}
	return r, nil
}

func (c *Client) login(ctx context.Context) error {
	r, err := c.post(ctx, "Login", "", map[string]any{"User": map[string]any{"Version": "0", "userName": c.User, "password": c.Pass}})
	if err != nil {
		if r.Error != nil && r.Cmd == "Login" {
			return errLogin
		}
		return err
	}
	var v struct {
		Token struct {
			Name      string `json:"name"`
			LeaseTime int    `json:"leaseTime"`
		} `json:"Token"`
	}
	if err := json.Unmarshal(r.Value, &v); err != nil || v.Token.Name == "" {
		return errors.New("reolink: no token in the login answer")
	}
	c.mu.Lock()
	c.token = v.Token.Name
	c.expiry = time.Now().Add(time.Duration(max(v.Token.LeaseTime-60, 60)) * time.Second)
	c.mu.Unlock()
	return nil
}

// tokenFor is a token that is still good, logging in again when it is not.
func (c *Client) tokenFor(ctx context.Context) (string, error) {
	c.mu.Lock()
	t, ok := c.token, time.Now().Before(c.expiry)
	c.mu.Unlock()
	if ok && t != "" {
		return t, nil
	}
	if err := c.login(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token, nil
}

func (c *Client) forget() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

// Channels is the recorder's cameras by name. A single camera answers with none, and is channel 0.
func (c *Client) Channels(ctx context.Context) ([]Channel, error) {
	t, err := c.tokenFor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := c.post(ctx, "GetChannelstatus", t, map[string]any{})
	if err != nil {
		c.forget()
		return []Channel{{Number: 0, Name: "Camera", Online: true}}, nil
	}
	var v struct {
		Status []struct {
			Channel int    `json:"channel"`
			Name    string `json:"name"`
			Online  int    `json:"online"`
		} `json:"status"`
	}
	if err := json.Unmarshal(r.Value, &v); err != nil {
		return nil, err
	}
	var out []Channel
	for _, s := range v.Status {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = fmt.Sprintf("Camera %d", s.Channel+1)
		}
		out = append(out, Channel{Number: s.Channel, Name: name, Online: s.Online == 1})
	}
	return out, nil
}

// Snap is one JPEG from a channel, asked for at about width pixels across where the recorder can
// scale it (a 4K picture takes this device's processor seconds to decode). A recorder that will not
// scale is asked again for its own size, with a fresh token in case that was the trouble.
func (c *Client) Snap(ctx context.Context, channel, width int) ([]byte, error) {
	var last error
	for _, w := range []int{width, 0} {
		t, err := c.tokenFor(ctx)
		if err != nil {
			return nil, err
		}
		u := fmt.Sprintf("%s/cgi-bin/api.cgi?cmd=Snap&channel=%d&rs=%x&token=%s", c.Base, channel, rand.Uint32(), t)
		if w > 0 {
			u += fmt.Sprintf("&width=%d&height=%d", w, w*9/16)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.client().Do(req)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxSnap))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusOK && len(b) > 3 && b[0] == 0xff && b[1] == 0xd8 {
			return b, nil
		}
		c.forget()
		last = fmt.Errorf("reolink: channel %d gave no picture", channel)
		if w == 0 {
			break
		}
	}
	return nil, last
}
