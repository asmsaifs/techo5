package onvifevent

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	clockReply = `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><tds:GetSystemDateAndTimeResponse><tds:SystemDateAndTime>
<tt:UTCDateTime><tt:Time><tt:Hour>17</tt:Hour><tt:Minute>8</tt:Minute><tt:Second>12</tt:Second></tt:Time><tt:Date><tt:Year>2026</tt:Year><tt:Month>10</tt:Month><tt:Day>10</tt:Day></tt:Date></tt:UTCDateTime>
<tt:LocalDateTime><tt:Time><tt:Hour>12</tt:Hour><tt:Minute>8</tt:Minute><tt:Second>12</tt:Second></tt:Time><tt:Date><tt:Year>2026</tt:Year><tt:Month>10</tt:Month><tt:Day>10</tt:Day></tt:Date></tt:LocalDateTime>
</tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse></s:Body></s:Envelope>`

	// A recorder names its services on an address of its own; the client asks on the one it reached.
	capsReply = `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><GetCapabilitiesResponse><Capabilities><tt:Events><tt:XAddr>http://198.51.100.9:8000/onvif/event_service</tt:XAddr></tt:Events></Capabilities></GetCapabilitiesResponse></s:Body></s:Envelope>`

	subReply = `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:wsa5="http://www.w3.org/2005/08/addressing"><s:Body><CreatePullPointSubscriptionResponse><SubscriptionReference><wsa5:Address>http://198.51.100.9:8000/onvif/PullSubManager?Idx=27566238</wsa5:Address></SubscriptionReference></CreatePullPointSubscriptionResponse></s:Body></s:Envelope>`

	// Two notifications as a Home Hub sends them for a press on its first channel.
	pullReply = `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" xmlns:tt="http://www.onvif.org/ver10/schema"><s:Body><PullMessagesResponse>
<wsnt:NotificationMessage><wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">tns1:RuleEngine/MyRuleDetector/Visitor</wsnt:Topic><wsnt:Message><tt:Message UtcTime="2026-10-10T17:08:12Z" PropertyOperation="Changed"><tt:Source><tt:SimpleItem Name="Source" Value="000"/></tt:Source><tt:Data><tt:SimpleItem Name="State" Value="true"/></tt:Data></tt:Message></wsnt:Message></wsnt:NotificationMessage>
<wsnt:NotificationMessage><wsnt:Topic Dialect="x">tns1:RuleEngine/MyRuleDetector/PeopleDetect</wsnt:Topic><wsnt:Message><tt:Message PropertyOperation="Initialized"><tt:Source><tt:SimpleItem Name="Source" Value="001"/></tt:Source><tt:Data><tt:SimpleItem Name="State" Value="false"/></tt:Data></tt:Message></wsnt:Message></wsnt:NotificationMessage>
</PullMessagesResponse></s:Body></s:Envelope>`

	faultReply = `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:ter="http://www.onvif.org/ver10/error"><s:Body><s:Fault><s:Code><s:Value>s:Sender</s:Value><s:Subcode><s:Value>ter:NotAuthorized</s:Value></s:Subcode></s:Code><s:Reason><s:Text xml:lang="en">Sender not Authorized</s:Text></s:Reason></s:Fault></s:Body></s:Envelope>`
)

// fake is a device that checks the digest against pass, and records what it was asked where.
type fake struct {
	pass string
	mu   sync.Mutex
	got  []string // "path action"
}

var (
	reNonce   = regexp.MustCompile(`<Nonce[^>]*>([^<]+)<`)
	reCreated = regexp.MustCompile(`<Created[^>]*>([^<]+)<`)
	reDigest  = regexp.MustCompile(`<Password[^>]*>([^<]+)<`)
)

func (f *fake) authed(body string) bool {
	n, c, d := reNonce.FindStringSubmatch(body), reCreated.FindStringSubmatch(body), reDigest.FindStringSubmatch(body)
	if n == nil || c == nil || d == nil {
		return false
	}
	nonce, _ := base64.StdEncoding.DecodeString(n[1])
	h := sha1.New()
	h.Write(nonce)
	h.Write([]byte(c[1]))
	h.Write([]byte(f.pass))
	return base64.StdEncoding.EncodeToString(h.Sum(nil)) == d[1]
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	body := string(b)
	action := regexp.MustCompile(`action="[^"]*/([A-Za-z]+)"`).FindStringSubmatch(r.Header.Get("Content-Type"))[1]
	f.mu.Lock()
	f.got = append(f.got, r.URL.RequestURI()+" "+action)
	f.mu.Unlock()
	if action != "GetSystemDateAndTime" && !f.authed(body) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, faultReply)
		return
	}
	switch action {
	case "GetSystemDateAndTime":
		io.WriteString(w, clockReply)
	case "GetCapabilities":
		io.WriteString(w, capsReply)
	case "CreatePullPointSubscriptionRequest":
		io.WriteString(w, subReply)
	case "PullMessagesRequest":
		if !strings.Contains(body, "PullSubManager?Idx=27566238") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		io.WriteString(w, pullReply)
	default:
		io.WriteString(w, `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body/></s:Envelope>`)
	}
}

func TestSubscribeAndPull(t *testing.T) {
	f := &fake{pass: "p<a&ss"}
	srv := httptest.NewServer(f)
	defer srv.Close()

	c := &Client{Base: srv.URL, User: "admin", Pass: "p<a&ss",
		now: func() time.Time { return time.Date(2026, 10, 10, 17, 0, 0, 0, time.UTC) }}
	ctx := context.Background()
	s, err := c.Subscribe(ctx, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if c.offset != 8*time.Minute+12*time.Second {
		t.Errorf("clock offset %v, want the device's UTC less ours", c.offset)
	}
	if !strings.HasPrefix(s.addr, srv.URL+"/onvif/PullSubManager?Idx=27566238") {
		t.Errorf("subscription asked at %q, not on the address the device was reached at", s.addr)
	}
	evs, err := s.Pull(ctx, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("%d events, want 2: %+v", len(evs), evs)
	}
	v := evs[0]
	if v.Topic != "tns1:RuleEngine/MyRuleDetector/Visitor" || v.Op != "Changed" || v.Items["Source"] != "000" || v.Items["State"] != "true" {
		t.Errorf("press read as %+v", v)
	}
	if evs[1].Op != "Initialized" || evs[1].Items["Source"] != "001" {
		t.Errorf("second event read as %+v", evs[1])
	}
	if err := s.Renew(ctx, 5*time.Minute); err != nil {
		t.Errorf("renew: %v", err)
	}
	if err := s.Close(ctx); err != nil {
		t.Errorf("close: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.got {
		if strings.Contains(g, "PullMessages") && !strings.HasPrefix(g, "/onvif/PullSubManager?Idx=27566238 ") {
			t.Errorf("pulled at %q", g)
		}
		if strings.Contains(g, "CreatePullPoint") && !strings.HasPrefix(g, "/onvif/event_service ") {
			t.Errorf("subscribed at %q", g)
		}
	}
}

func TestWrongPassword(t *testing.T) {
	srv := httptest.NewServer(&fake{pass: "right"})
	defer srv.Close()
	c := &Client{Base: srv.URL, User: "admin", Pass: "wrong"}
	if _, err := c.Subscribe(context.Background(), time.Minute); !errors.Is(err, ErrAuth) {
		t.Fatalf("got %v, want ErrAuth", err)
	}
}

func TestBadAddress(t *testing.T) {
	c := &Client{Base: "nonsense"}
	if _, err := c.Subscribe(context.Background(), time.Minute); err == nil {
		t.Fatal("subscribed to nothing")
	}
}

func TestOnBaseKeepsTheHost(t *testing.T) {
	c := &Client{Base: "http://192.0.2.5:8000"}
	for in, want := range map[string]string{
		"http://198.51.100.1:8000/onvif/PullSubManager?Idx=1": "http://192.0.2.5:8000/onvif/PullSubManager?Idx=1",
		"https://evil.example/steal":                          "http://192.0.2.5:8000/steal",
		"/relative?x=1":                                       "http://192.0.2.5:8000/relative?x=1",
		"@evil.example/x":                                     "http://192.0.2.5:8000/@evil.example/x",
		"@198.51.100.7/x":                                     "http://192.0.2.5:8000/@198.51.100.7/x",
		"http://user:pw@evil.example/x?y=1":                   "http://192.0.2.5:8000/x?y=1",
		"//evil.example/x":                                    "http://192.0.2.5:8000/x",
	} {
		if got := c.onBase(in); got != want {
			t.Errorf("onBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFaultAndDuration(t *testing.T) {
	if got := fault([]byte(faultReply)); got != "Sender not Authorized" {
		t.Errorf("fault reason %q", got)
	}
	if got := duration(1500 * time.Millisecond); got != "PT1S" {
		t.Errorf("duration %q", got)
	}
	if got := duration(0); got != "PT1S" {
		t.Errorf("zero duration %q", got)
	}
}
