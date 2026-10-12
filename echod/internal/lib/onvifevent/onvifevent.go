// Package onvifevent follows a camera's or recorder's events over ONVIF, by pull point: the device is
// asked for a subscription, then asked again and again for whatever has happened since, each ask
// waiting up to a timeout for something to happen. It is what a doorbell press arrives by from a
// Reolink Home Hub (feature/doorbell), with no Home Assistant in between.
//
// A pull point rather than a push (a Notify sent to an address of ours) because the device then
// needs no way back in: nothing listens on this side, and a recorder that cannot reach the device,
// or a firewall in between, changes nothing. ONVIF over plain HTTP, as every camera that has it
// serves it, with the login sent as a WS-Security digest.
package onvifevent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxReply is the most of a reply read. A recorder's answers are a few kilobytes even with every
// channel's events in them; anything far past that is not an answer to keep reading. maxEvents is
// the most events taken from one, as many as a pull asks for.
const (
	maxReply  = 1 << 20
	maxEvents = 32
)

// ErrAuth is a device that refused the login.
var ErrAuth = errors.New("the login was refused")

// Client is one device, at Base ("http://host:port").
type Client struct {
	Base string
	User string
	Pass string

	// HTTP is the client asked with; nil is one with no timeout of its own, since a pull waits.
	// Redirects are never followed, whichever: a redirected POST carries the digest with it.
	HTTP *http.Client

	offset time.Duration // the device's clock less ours, for the digest's timestamp
	now    func() time.Time
}

// Event is one notification: its topic ("tns1:RuleEngine/MyRuleDetector/Visitor"), whether the
// property was Initialized, Changed or Deleted, and its simple items, from the source (which channel)
// and the data (State=true) together.
type Event struct {
	Topic string
	Op    string
	Items map[string]string
}

// Subscription is one pull point on the device.
type Subscription struct {
	c    *Client
	addr string // where it is asked, on Base's host whatever the device said
}

func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *Client) http() *http.Client {
	var hc http.Client
	if c.HTTP != nil {
		hc = *c.HTTP
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &hc
}

// Subscribe sets the clock, finds the events service and opens a pull point that lasts for life
// unless renewed.
func (c *Client) Subscribe(ctx context.Context, life time.Duration) (*Subscription, error) {
	base, err := url.Parse(c.Base)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("not an address: %q", c.Base)
	}
	if err := c.syncClock(ctx); err != nil {
		return nil, err
	}
	events := c.Base + "/onvif/event_service"
	if r, err := c.call(ctx, c.Base+"/onvif/device_service", "http://www.onvif.org/ver10/device/wsdl/GetCapabilities",
		`<GetCapabilities xmlns="http://www.onvif.org/ver10/device/wsdl"><Category>Events</Category></GetCapabilities>`, true, ""); err == nil {
		if a := eventsAddr(r); a != "" {
			events = c.onBase(a)
		}
	} else if errors.Is(err, ErrAuth) {
		return nil, err
	}
	r, err := c.call(ctx, events, "http://www.onvif.org/ver10/events/wsdl/EventPortType/CreatePullPointSubscriptionRequest",
		`<CreatePullPointSubscription xmlns="http://www.onvif.org/ver10/events/wsdl"><InitialTerminationTime>`+duration(life)+
			`</InitialTerminationTime></CreatePullPointSubscription>`, true, "")
	if err != nil {
		return nil, fmt.Errorf("subscribing: %w", err)
	}
	addr := first(r, "Address")
	if addr == "" {
		return nil, errors.New("subscribing: the device gave no address")
	}
	return &Subscription{c: c, addr: c.onBase(addr)}, nil
}

// Pull waits up to wait for events and returns those that came, none if nothing happened.
func (s *Subscription) Pull(ctx context.Context, wait time.Duration) ([]Event, error) {
	r, err := s.c.call(ctx, s.addr, "http://www.onvif.org/ver10/events/wsdl/PullPointSubscription/PullMessagesRequest",
		`<PullMessages xmlns="http://www.onvif.org/ver10/events/wsdl"><Timeout>`+duration(wait)+
			`</Timeout><MessageLimit>`+strconv.Itoa(maxEvents)+`</MessageLimit></PullMessages>`, true, s.to())
	if err != nil {
		return nil, err
	}
	return parseEvents(r)
}

// Renew keeps the subscription for life from now.
func (s *Subscription) Renew(ctx context.Context, life time.Duration) error {
	_, err := s.c.call(ctx, s.addr, "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewRequest",
		`<Renew xmlns="http://docs.oasis-open.org/wsn/b-2"><TerminationTime>`+duration(life)+`</TerminationTime></Renew>`, true, s.to())
	return err
}

// Close ends the subscription on the device. One left open runs out by itself at its termination
// time; closing it frees the device's slot now, which a recorder keeps few of.
func (s *Subscription) Close(ctx context.Context) error {
	_, err := s.c.call(ctx, s.addr, "http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeRequest",
		`<Unsubscribe xmlns="http://docs.oasis-open.org/wsn/b-2"/>`, true, s.to())
	return err
}

func (s *Subscription) to() string {
	return `<a:To xmlns:a="http://www.w3.org/2005/08/addressing">` + escape(s.addr) + `</a:To>`
}

// onBase is an address the device gave, moved onto the host it is reached at. A device behind a
// recorder or a NAT can name an address of its own that does not reach it from here, and a reply
// has no business sending the login anywhere else. Only the path and query are taken from it, the
// result is read back, and anything that does not land on Base's own host is the events service's
// usual address instead: an address of "@elsewhere/x" pasted onto Base as text would put Base in the
// userinfo and elsewhere in the host.
func (c *Client) onBase(addr string) string {
	base, err := url.Parse(c.Base)
	fallback := strings.TrimRight(c.Base, "/") + "/onvif/event_service"
	if err != nil || base.Host == "" {
		return fallback
	}
	u, err := url.Parse(strings.TrimSpace(addr))
	if err != nil {
		return fallback
	}
	p := u.Path
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	out := (&url.URL{Scheme: base.Scheme, Host: base.Host, Path: p, RawQuery: u.RawQuery}).String()
	back, err := url.Parse(out)
	if err != nil || back.Host != base.Host || back.User != nil || back.Scheme != base.Scheme {
		return fallback
	}
	return out
}

// syncClock reads the device's clock, which needs no login: the digest carries a timestamp the
// device checks against its own, and a device whose clock is out refuses an honest login.
func (c *Client) syncClock(ctx context.Context) error {
	r, err := c.call(ctx, c.Base+"/onvif/device_service", "http://www.onvif.org/ver10/device/wsdl/GetSystemDateAndTime",
		`<GetSystemDateAndTime xmlns="http://www.onvif.org/ver10/device/wsdl"/>`, false, "")
	if err != nil {
		return fmt.Errorf("reading the clock: %w", err)
	}
	if t, ok := utcTime(r); ok {
		c.offset = t.Sub(c.clock().UTC())
	}
	return nil
}

func (c *Client) security() string {
	nonce := make([]byte, 16)
	rand.Read(nonce)
	created := c.clock().Add(c.offset).UTC().Format("2006-01-02T15:04:05Z")
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(created))
	h.Write([]byte(c.Pass))
	return `<Security s:mustUnderstand="1" xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"><UsernameToken><Username>` +
		escape(c.User) + `</Username><Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` +
		base64.StdEncoding.EncodeToString(h.Sum(nil)) +
		`</Password><Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-soap-message-security-1.0#Base64Binary">` +
		base64.StdEncoding.EncodeToString(nonce) +
		`</Nonce><Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">` +
		created + `</Created></UsernameToken></Security>`
}

func (c *Client) call(ctx context.Context, addr, action, body string, auth bool, header string) ([]byte, error) {
	if auth {
		header = c.security() + header
	}
	env := `<?xml version="1.0" encoding="UTF-8"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">`
	if header != "" {
		env += `<s:Header>` + header + `</s:Header>`
	}
	env += `<s:Body>` + body + `</s:Body></s:Envelope>`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, addr, strings.NewReader(env))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `application/soap+xml; charset=utf-8; action="`+action+`"`)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return b, nil
	}
	reason := fault(b)
	if resp.StatusCode == http.StatusUnauthorized || strings.Contains(strings.ToLower(reason), "not authorized") {
		return nil, ErrAuth
	}
	if reason != "" {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, reason)
	}
	return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
}

// duration is d as an XML duration, in whole seconds.
func duration(d time.Duration) string {
	s := int(d / time.Second)
	if s < 1 {
		s = 1
	}
	return "PT" + strconv.Itoa(s) + "S"
}

func escape(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// walk calls f on every element start in r, with a way to read the element's text.
func walk(r []byte, f func(d *xml.Decoder, se xml.StartElement) error) error {
	d := xml.NewDecoder(bytes.NewReader(r))
	d.Strict = false
	for {
		t, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if se, ok := t.(xml.StartElement); ok {
			if err := f(d, se); err != nil {
				return err
			}
		}
	}
}

// text reads an element's own text, up to its end, skipping any elements inside.
func text(d *xml.Decoder) string {
	var b strings.Builder
	depth := 0
	for {
		t, err := d.Token()
		if err != nil {
			return strings.TrimSpace(b.String())
		}
		switch t := t.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth == 0 {
				return strings.TrimSpace(b.String())
			}
			depth--
		case xml.CharData:
			if depth == 0 {
				b.Write(t)
			}
		}
	}
}

var errFound = errors.New("found")

// first is the text of the first element named local.
func first(r []byte, local string) string {
	var v string
	walk(r, func(d *xml.Decoder, se xml.StartElement) error {
		if se.Name.Local == local {
			v = text(d)
			return errFound
		}
		return nil
	})
	return v
}

// eventsAddr is the events service's XAddr in a GetCapabilities reply.
func eventsAddr(r []byte) string {
	var v string
	inEvents := false
	walk(r, func(d *xml.Decoder, se xml.StartElement) error {
		switch se.Name.Local {
		case "Events":
			inEvents = true
		case "XAddr":
			if inEvents {
				v = text(d)
				return errFound
			}
		case "Analytics", "Device", "Imaging", "Media", "PTZ", "Extension":
			inEvents = false
		}
		return nil
	})
	return v
}

// fault is a SOAP fault's reason, or its subcode where it gives none.
func fault(r []byte) string {
	var reason, code string
	walk(r, func(d *xml.Decoder, se xml.StartElement) error {
		switch se.Name.Local {
		case "Text":
			if reason == "" {
				reason = text(d)
			}
		case "Value":
			code = text(d) // the last, innermost subcode
		}
		return nil
	})
	if reason == "" {
		reason = code
	}
	if len(reason) > 200 {
		reason = reason[:200]
	}
	return reason
}

// utcTime is the UTCDateTime in a GetSystemDateAndTime reply.
func utcTime(r []byte) (time.Time, bool) {
	vals := map[string]int{}
	in := false
	walk(r, func(d *xml.Decoder, se xml.StartElement) error {
		switch se.Name.Local {
		case "UTCDateTime":
			in = true
		case "LocalDateTime":
			in = false
		case "Year", "Month", "Day", "Hour", "Minute", "Second":
			if in {
				if n, err := strconv.Atoi(text(d)); err == nil {
					vals[se.Name.Local] = n
				}
			}
		}
		return nil
	})
	if vals["Year"] < 2000 || vals["Month"] < 1 || vals["Day"] < 1 {
		return time.Time{}, false
	}
	return time.Date(vals["Year"], time.Month(vals["Month"]), vals["Day"], vals["Hour"], vals["Minute"], vals["Second"], 0, time.UTC), true
}

// parseEvents is the notifications in a PullMessages reply.
func parseEvents(r []byte) ([]Event, error) {
	var out []Event
	var cur *Event
	err := walk(r, func(d *xml.Decoder, se xml.StartElement) error {
		switch se.Name.Local {
		case "NotificationMessage":
			if len(out) >= maxEvents {
				cur = nil // past what was asked for: not read
				return nil
			}
			out = append(out, Event{Items: map[string]string{}})
			cur = &out[len(out)-1]
		case "Topic":
			if cur != nil {
				cur.Topic = text(d)
			}
		case "Message":
			if cur != nil {
				for _, a := range se.Attr {
					if a.Name.Local == "PropertyOperation" {
						cur.Op = a.Value
					}
				}
			}
		case "SimpleItem":
			if cur != nil {
				var name, value string
				for _, a := range se.Attr {
					switch a.Name.Local {
					case "Name":
						name = a.Value
					case "Value":
						value = a.Value
					}
				}
				if name != "" && len(cur.Items) < 32 {
					cur.Items[name] = value
				}
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("reading the events: %w", err)
	}
	return out, nil
}
