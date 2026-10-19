package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

func TestParsePages(t *testing.T) {
	got, err := parsePages("Status=https://status.example/board,\nArt = http://art.example:8080/feed?key=k|refresh=10m\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].name != "Status" || got[0].addr != "https://status.example/board" || got[0].refresh != 0 {
		t.Fatalf("first page %+v", got)
	}
	if got[1].name != "Art" || got[1].addr != "http://art.example:8080/feed?key=k" || got[1].refresh != 10*time.Minute {
		t.Fatalf("second page %+v", got[1])
	}
	if got, err := parsePages(""); err != nil || len(got) != 0 {
		t.Errorf("no pages: %v, %v", got, err)
	}
	for _, bad := range []string{
		"Status",                                            // no address
		"=https://x.example",                                // no name
		"Status=file:///etc/passwd",                         // not the web
		"Status=chrome://settings",                          // not the web
		"Status=javascript:alert(1)",                        // not the web
		"Status=https://user:pw@x.example",                  // a user in it
		"Status=https://x.example,status=https://y.example", // twice, however it is cased
		"St/atus=https://x.example",                         // a name a list cannot show
		"Status=https://x.example|refresh=5s",               // too often
		"Status=https://x.example|refresh=soon",             // not a time
		"Status=https://x.example|zoom=2",                   // no such option
		"Status=https:///nohost",                            // no host
	} {
		if _, err := parsePages(bad); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
}

func TestFindPage(t *testing.T) {
	pages, _ := parsePages("Status=https://status.example, Art=https://art.example")
	if p, ok := findPage(pages, " status "); !ok || p.name != "Status" {
		t.Errorf("found %+v, %v", p, ok)
	}
	if _, ok := findPage(pages, "https://status.example"); ok {
		t.Error("a page was found by its address")
	}
	if _, ok := findPage(pages, "Nothing"); ok {
		t.Error("a page not listed was found")
	}
	if got := pageNames(pages); !slices.Equal(got, []string{"Status", "Art"}) {
		t.Errorf("names %v", got)
	}
}

func TestLoggedIsTheSiteAlone(t *testing.T) {
	if got := logged("https://art.example/share/s3cr3t-token/feed?key=secret#x"); got != "https://art.example" {
		t.Errorf("logged %q", got)
	}
}

// An entry that is refused is named by its place or its name, never by its address.
func TestParseErrorsLeaveOutTheAddress(t *testing.T) {
	for _, bad := range []string{"https://x.example/s3cr3t", "Status=ftp://x.example/s3cr3t", "Bad/Name=https://x.example/s3cr3t"} {
		_, err := parsePages(bad)
		if err == nil || strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := parsePages("Status=https://bücher.example/"); err == nil {
		t.Error("a host not in its ASCII form was taken")
	}
}

func TestOnSite(t *testing.T) {
	home := pageOrigin("https://Status.Example:443/board")
	if home != "https://status.example" {
		t.Fatalf("origin %q", home)
	}
	for _, a := range []string{"https://status.example/board", "https://STATUS.example:443/x", "", "about:blank"} {
		if !onSite(a, home) {
			t.Errorf("%q is not the page's site", a)
		}
	}
	for _, a := range []string{"https://other.example/", "http://status.example/", "https://status.example:8443/",
		"data:text/html,hi", "blob:https://status.example/1", "chrome-error://chromewebdata/", "about:srcdoc", "file:///etc/passwd"} {
		if onSite(a, home) {
			t.Errorf("%q was taken for the page's site", a)
		}
	}
}

// talk is a device's side of one connection to serve: the hello sent, and the first message back.
func talk(t *testing.T, cfg config, h hello) (kind byte, payload []byte) {
	t.Helper()
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		serve(ctx, nil, &guard{cfg: cfg}, cfg, b)
	}()
	c, err := clientHandshake(a, cfg.key)
	if err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(h)
	if _, err := c.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		t.Fatal(err)
	}
	msg := make([]byte, binary.BigEndian.Uint32(hdr[:]))
	if _, err := io.ReadFull(r, msg); err != nil {
		t.Fatal(err)
	}
	a.Close()
	<-done
	return msg[0], msg[1:]
}

// A device can ask for the web pages' names, and is told nothing else.
func TestAHelloAsksForThePages(t *testing.T) {
	pages, _ := parsePages("Status=https://status.example/?key=k, Art=https://art.example")
	cfg := config{key: "a-long-enough-test-key", pages: pages}
	kind, payload := talk(t, cfg, hello{Name: "Kitchen", List: true})
	var names []string
	if kind != kindPages || json.Unmarshal(payload, &names) != nil || !slices.Equal(names, []string{"Status", "Art"}) {
		t.Fatalf("kind %d, %q", kind, payload)
	}
	if strings.Contains(string(payload), "example") {
		t.Error("an address went to the device")
	}
	// A server that lists none says so with an empty list.
	kind, payload = talk(t, config{key: "a-long-enough-test-key"}, hello{Name: "Kitchen", List: true})
	if kind != kindPages || string(payload) != "[]" {
		t.Errorf("kind %d, %q", kind, payload)
	}
}

// A name that could be no page's is refused with a sentence of the server's own, not echoed.
func TestAStrangeNameIsNotEchoed(t *testing.T) {
	cfg := config{key: "a-long-enough-test-key"}
	kind, payload := talk(t, cfg, hello{Name: "Kitchen", W: 480, H: 480, Page: "<b>https://evil.example/" + strings.Repeat("x", 200)})
	if kind != kindProblem || strings.Contains(string(payload), "evil") {
		t.Errorf("kind %d, %q", kind, payload)
	}
}

// A page not listed is refused by name, before any browser is asked for anything.
func TestAPageNotListedIsRefused(t *testing.T) {
	pages, _ := parsePages("Status=https://status.example")
	cfg := config{key: "a-long-enough-test-key", pages: pages}
	kind, payload := talk(t, cfg, hello{Name: "Kitchen", W: 480, H: 480, Page: "Nothing"})
	if kind != kindProblem || !strings.Contains(string(payload), "no web page called Nothing") {
		t.Errorf("kind %d, %q", kind, payload)
	}
}

// A web page opens in a context of its own, with nothing of Home Assistant's sign-in in it, and
// cannot open another window. Run where TestOpenATabInTheRunningBrowser runs.
func TestAPageOpensApart(t *testing.T) {
	required := os.Getenv("DASHCAST_REQUIRE_BROWSER") != ""
	if chromePath("") == "" {
		if required {
			t.Fatal("no Chrome or headless-shell to run")
		}
		t.Skip("no Chrome or headless-shell to run")
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><title>status</title><p>all well"))
	}))
	defer site.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := newBrowser(ctx, config{ha: site.URL, token: "the-home-assistant-token"})
	if err != nil {
		if required {
			t.Fatalf("the browser did not start: %v", err)
		}
		t.Skipf("the browser did not start: %v", err)
	}
	defer b.close()

	tab, closeTab, err := b.openPage(ctx, webPage{name: "Status", addr: site.URL}, 480, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTab()
	var got struct {
		Tokens string `json:"tokens"`
		Popup  bool   `json:"popup"`
	}
	if err := chromedp.Run(tab, chromedp.Evaluate(`({tokens: localStorage.getItem("hassTokens") || "", popup: window.open("about:blank") !== null})`, &got)); err != nil {
		t.Fatal(err)
	}
	if got.Tokens != "" || got.Popup {
		t.Errorf("tokens %q, popup %v", got.Tokens, got.Popup)
	}
	// A window opened in the page's context some other way - here by asking the browser for one, as a
	// form or a frame might - is closed as it appears.
	bctx, _ := b.pagesContext(t)
	var extra target.ID
	if err := chromedp.Run(b.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		extra, err = target.CreateTarget("about:blank").WithBrowserContextID(bctx).Do(asBrowser(ctx))
		return err
	})); err != nil {
		t.Fatal(err)
	}
	waitGone(t, b, extra)
	// And the dashboards' tabs see nothing the page keeps.
	if err := chromedp.Run(tab, chromedp.Evaluate(`localStorage.setItem("mark", "page"), document.cookie = "mark=page"`, nil)); err != nil {
		t.Fatal(err)
	}
	dash, closeDash, err := b.open(ctx, "/lovelace/0", 480, 480, map[string]bool{"lovelace": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDash()
	var mark string
	if err := chromedp.Run(dash, chromedp.Evaluate(`(localStorage.getItem("mark") || "") + document.cookie`, &mark)); err != nil {
		t.Fatal(err)
	}
	if mark != "" {
		t.Errorf("the dashboard's tab sees the page's %q", mark)
	}
}

// A web page taken to another site is brought back to its own.
func TestAPageIsKeptOnItsSite(t *testing.T) {
	required := os.Getenv("DASHCAST_REQUIRE_BROWSER") != ""
	if chromePath("") == "" {
		if required {
			t.Fatal("no Chrome or headless-shell to run")
		}
		t.Skip("no Chrome or headless-shell to run")
	}
	site := func(title string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>" + title + "</title><p>" + title))
		}))
	}
	home, away := site("home"), site("away")
	defer home.Close()
	defer away.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := newBrowser(ctx, config{ha: home.URL})
	if err != nil {
		if required {
			t.Fatalf("the browser did not start: %v", err)
		}
		t.Skipf("the browser did not start: %v", err)
	}
	defer b.close()
	p := webPage{name: "Status", addr: home.URL}
	tab, closeTab, err := b.openPage(ctx, p, 480, 480)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTab()
	kctx, stop := context.WithCancel(ctx)
	defer stop()
	go keepOnPage(kctx, tab, p, "test")
	// The page sending itself away is refused before it goes; the title never changes.
	if err := chromedp.Run(tab, chromedp.Evaluate(`location.href = "`+away.URL+`"`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	var title string
	if err := chromedp.Run(tab, chromedp.Title(&title)); err != nil || title != "home" {
		t.Errorf("after the page sent itself away: %q, %v", title, err)
	}
	// One that gets there anyway (here, sent by dashcast's own command, which nothing refuses) is
	// brought back.
	_ = chromedp.Run(tab, chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, _, _, err := page.Navigate(away.URL).Do(ctx)
		return err
	}))
	deadline := time.Now().Add(10 * time.Second)
	for {
		var title string
		_ = chromedp.Run(tab, chromedp.Title(&title))
		if title == "home" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the page stayed on %q", title)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// pagesContext is the one web page's context the browser has open, for a test.
func (b *browser) pagesContext(t *testing.T) (cdp.BrowserContextID, target.ID) {
	t.Helper()
	var id cdp.BrowserContextID
	var tid target.ID
	n := 0
	b.pages.Range(func(k, v any) bool {
		id, tid = k.(cdp.BrowserContextID), v.(target.ID)
		n++
		return true
	})
	if n != 1 {
		t.Fatalf("%d page contexts", n)
	}
	return id, tid
}

// waitGone waits for a window to be closed.
func waitGone(t *testing.T, b *browser, id target.ID) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var infos []*target.Info
		_ = chromedp.Run(b.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			infos, err = target.GetTargets().Do(asBrowser(ctx))
			return err
		}))
		if !slices.ContainsFunc(infos, func(i *target.Info) bool { return i.TargetID == id }) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("a popup in a web page's context was left open")
}

// A listed address that lands on another site is refused on its first load, and the page fails to
// open saying where it lands, rather than being taken back and forth.
func TestAnAddressThatLandsElsewhere(t *testing.T) {
	required := os.Getenv("DASHCAST_REQUIRE_BROWSER") != ""
	if chromePath("") == "" {
		if required {
			t.Fatal("no Chrome or headless-shell to run")
		}
		t.Skip("no Chrome or headless-shell to run")
	}
	away := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><title>away</title>"))
	}))
	defer away.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, away.URL+"/secret-path", http.StatusFound)
	}))
	defer home.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := newBrowser(ctx, config{ha: home.URL})
	if err != nil {
		if required {
			t.Fatalf("the browser did not start: %v", err)
		}
		t.Skipf("the browser did not start: %v", err)
	}
	defer b.close()
	_, _, err = b.openPage(ctx, webPage{name: "Status", addr: home.URL}, 480, 480)
	if err == nil || !strings.Contains(err.Error(), pageOrigin(away.URL)) || strings.Contains(err.Error(), "secret-path") {
		t.Errorf("opened: %v", err)
	}
	n := 0
	b.pages.Range(func(any, any) bool { n++; return true })
	if n != 0 {
		t.Errorf("%d page contexts left behind", n)
	}
}
