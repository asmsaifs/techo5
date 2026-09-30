package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
)

// Looking things up: a search through a SearXNG server (config.Brain.Search), and reading a page it
// found, for what the model cannot know - a game's date, a store's hours, the news. Without a search
// server set the tools are not offered, and the model is told it cannot look anything up.

const (
	// searchResults is how many results go back to the model: enough to find the page that answers,
	// few enough not to fill its context with snippets.
	searchResults = 6

	// pageChars is how much of a page's text goes back: a schedule or an article's opening, within what
	// the model can read quickly.
	pageChars = 6000

	// pageMax bounds what is downloaded of a page.
	pageMax = 3 << 20
)

var webClient = &http.Client{Timeout: 15 * time.Second}

func webTools() []tool {
	if config.Get().Brain.Search == "" {
		return nil
	}
	return []tool{
		{llm.Tool{Name: "web_search", Description: "Search the web. Use it for anything current or that you are not sure of: sports schedules and scores, news, weather elsewhere, business hours, prices. Returns titles, addresses and snippets; read a page for details.",
			Parameters: object(map[string]any{"query": str("What to search for, as you would type it into a search engine.")}, "query")},
			func(a map[string]any) (string, error) { return search(argString(a, "query")) }},

		{llm.Tool{Name: "read_page", Description: "Read the text of a web page, such as one web_search found. Schedules on espn.com read well.",
			Parameters: object(map[string]any{"url": str("The page's address.")}, "url")},
			func(a map[string]any) (string, error) { return readPage(argString(a, "url")) }},
	}
}

func search(q string) (string, error) {
	if q == "" {
		return "", errors.New("nothing to search for")
	}
	base := strings.TrimRight(config.Get().Brain.Search, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/search?"+url.Values{"q": {q}, "format": {"json"}}.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := webClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("the search server did not answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the search server answered %s (is its JSON format turned on?)", resp.Status)
	}
	var out struct {
		Results []struct {
			Title, URL, Content string
		} `json:"results"`
		Answers []any `json:"answers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return "", fmt.Errorf("the search server's answer was not JSON")
	}
	var s []string
	for _, a := range out.Answers {
		if t, ok := a.(string); ok {
			s = append(s, "answer: "+t)
		}
	}
	for i, r := range out.Results {
		if i >= searchResults {
			break
		}
		s = append(s, fmt.Sprintf("%d. %s <%s> %s", i+1, r.Title, r.URL, strings.Join(strings.Fields(r.Content), " ")))
	}
	if len(s) == 0 {
		return "no results", nil
	}
	return strings.Join(s, "\n"), nil
}

func readPage(addr string) (string, error) {
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%q is not a web address", addr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	// Some sites send nothing to a client that does not look like a browser.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux armv7l) TECHO5 voice assistant")
	resp, err := webClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s did not answer", u.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", u.Host, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, pageMax))
	if err != nil {
		return "", err
	}
	text := pageText(string(b))
	if text == "" {
		return "the page has no text to read (it may only fill itself in with scripts); try another result", nil
	}
	if len(text) > pageChars {
		text = text[:pageChars] + " ..."
	}
	return "The page says (answer only from this; if it does not say, read another result):\n" + text, nil
}

// pageText is a page's readable text: its main content when it marks one, without scripts, menus,
// headers, footers and sidebars, a line for each block. A sports site's schedule page is some twenty
// thousand characters of menus before the schedule, and only the first part of a page goes back to the
// model: it answered from what it had not seen.
func pageText(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return ""
	}
	root := doc
	if m := find(doc, "main"); m != nil && len(textOf(m)) > 500 {
		root = m
	}
	return textOf(root)
}

// skipped are elements whose text is never what the page is about.
var skipped = map[string]bool{"script": true, "style": true, "noscript": true, "svg": true, "head": true,
	"nav": true, "header": true, "footer": true, "aside": true, "form": true, "template": true, "iframe": true}

// blocks end a line.
var blocks = map[string]bool{"p": true, "div": true, "li": true, "tr": true, "br": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "section": true, "article": true, "table": true, "dt": true, "dd": true}

func find(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := find(c, tag); f != nil {
			return f
		}
	}
	return nil
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && skipped[n.Data] {
			return
		}
		if n.Type == html.TextNode {
			if t := strings.Join(strings.Fields(n.Data), " "); t != "" {
				b.WriteString(t)
				b.WriteByte(' ')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blocks[n.Data] {
			b.WriteByte('\n')
		}
	}
	walk(n)
	var lines []string
	for _, l := range strings.Split(b.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}
