package remote

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// How a request proves it was made with the key. The key is shown once, on the setup page (the one time
// it crosses the network), and not kept: the device keeps the secret derived from it (SecretOf), which is
// what both ends sign with. Every request is signed, over its method, its path and query, when it was
// made, a nonce and its body, so one seen on the network cannot be sent again or changed.
//
// A client signs like this (Python), and sends header as X-Techo5-Remote:
//
//	secret = hmac.new(b"techo5-remote v1", key.encode(), hashlib.sha256).digest()
//	ts, nonce = str(int(time.time())), secrets.token_hex(12)
//	msg = "\n".join(["v1", method, path_and_query, ts, nonce, hashlib.sha256(body).hexdigest()]) + "\n"
//	header = f"v1 {ts} {nonce} " + hmac.new(secret, msg.encode(), hashlib.sha256).hexdigest()
//
// The secret can sign as well as check, so it is a secret too: it stays in the device's settings file,
// which only the daemon reads, and never goes out in the diagnostics download or a settings file.

const (
	authHeader = "X-Techo5-Remote"
	keyLabel   = "techo5-remote v1"

	// skew is how far a client's clock may be from this one's.
	skew = 30 * time.Second

	// noncesKept bounds the nonces remembered within the skew.
	noncesKept = 4096

	// failures is how many requests that do not check out one address may make in failWindow before it is
	// turned away until the window ends. Only failures count, and only the address making them is
	// turned away, so a stranger cannot lock the real client out.
	failures   = 10
	failWindow = time.Minute

	// addressesKept bounds the addresses whose failures are counted.
	addressesKept = 1024
)

// NewKey is a new key, to be shown once.
func NewKey() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// SecretOf is the signing secret of a key, hex, which is what the device keeps.
func SecretOf(key string) string {
	m := hmac.New(sha256.New, []byte(keyLabel))
	m.Write([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(m.Sum(nil))
}

// signature is the signature of a request with the secret (hex).
func signature(secretHex, method, pathQuery, ts, nonce string, body []byte) ([]byte, error) {
	secret, err := hex.DecodeString(secretHex)
	if err != nil || len(secret) == 0 {
		return nil, errNoKey
	}
	sum := sha256.Sum256(body)
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("v1\n" + method + "\n" + pathQuery + "\n" + ts + "\n" + nonce + "\n" + hex.EncodeToString(sum[:]) + "\n"))
	return m.Sum(nil), nil
}

// Sign is the header for a request, for a client written in Go and for the tests.
func Sign(key, method, pathQuery string, body []byte, now time.Time) string {
	var n [12]byte
	_, _ = rand.Read(n[:])
	ts, nonce := strconv.FormatInt(now.Unix(), 10), hex.EncodeToString(n[:])
	sig, _ := signature(SecretOf(key), method, pathQuery, ts, nonce, body)
	return "v1 " + ts + " " + nonce + " " + hex.EncodeToString(sig)
}

var (
	errNoKey    = errors.New("no key has been made")
	errUnsigned = errors.New("not signed with this device's key")
	errStale    = errors.New("signed at a time too far from this device's clock")
	errReplayed = errors.New("a request already taken")
)

// looksSigned is whether a header has the shape of a signature, which is all that can be told before the
// body it covers has been read.
func looksSigned(header string) bool {
	parts := strings.Fields(header)
	return len(parts) == 4 && parts[0] == "v1" && len(parts[3]) == 64
}

// inTime is whether a signed-looking header's time is one this device would take: within the skew,
// and not from before the program started (started): the nonces remembered are gone after a restart,
// and this keeps one taken before it from being sent again. Checked before the body is read.
func inTime(header string, now time.Time) bool {
	parts := strings.Fields(header)
	if len(parts) != 4 {
		return false
	}
	sec, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return false
	}
	at := time.Unix(sec, 0)
	d := now.Sub(at)
	return d <= skew && d >= -skew && !at.Before(startedWall(now))
}

// startedWall is when this program started, by the wall clock as it reads at now: worked out from the
// monotonic clock, so a clock set back after the start (by NTP, say) does not put the start ahead of
// every request until the time catches up.
func startedWall(now time.Time) time.Time { return now.Add(-time.Since(started)).Truncate(time.Second) }

// verify checks a request's signature.
func verify(secretHex, header, method, pathQuery string, body []byte, now time.Time) error {
	if secretHex == "" {
		return errNoKey
	}
	parts := strings.Fields(header)
	if len(parts) != 4 || parts[0] != "v1" {
		return errUnsigned
	}
	want, err := signature(secretHex, method, pathQuery, parts[1], parts[2], body)
	if err != nil {
		return err
	}
	got, err := hex.DecodeString(parts[3])
	if err != nil || !hmac.Equal(got, want) {
		return errUnsigned
	}
	sec, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return errUnsigned
	}
	if d := now.Sub(time.Unix(sec, 0)); d > skew || d < -skew || time.Unix(sec, 0).Before(startedWall(now)) {
		return errStale
	}
	if !fresh(parts[2], now) {
		return errReplayed
	}
	return nil
}

var nonces struct {
	sync.Mutex
	seen map[string]time.Time
}

func fresh(nonce string, now time.Time) bool {
	nonces.Lock()
	defer nonces.Unlock()
	if nonces.seen == nil {
		nonces.seen = map[string]time.Time{}
	}
	if _, ok := nonces.seen[nonce]; ok {
		return false
	}
	if len(nonces.seen) >= noncesKept {
		for k, at := range nonces.seen {
			if now.Sub(at) > 2*skew {
				delete(nonces.seen, k)
			}
		}
		if len(nonces.seen) >= noncesKept {
			return false
		}
	}
	nonces.seen[nonce] = now
	return true
}

// tries counts failed requests by the address they came from.
var tries struct {
	sync.Mutex
	by map[string]*tally
}

type tally struct {
	n    int
	from time.Time
}

// source is the address a request came from, without its port, for counting its failures. A global IPv6
// address is counted by its /64, since a host there has the whole of it to pick addresses from; a
// link-local or private one (fe80::/10, fc00::/7) by itself, since every device in the home shares its
// /64 and one of them failing must not turn the rest away.
func source(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil && !ip.IsLinkLocalUnicast() && !ip.IsPrivate() {
		return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	}
	return host
}

// turnedAway is whether an address has failed too often in the window it is in.
func turnedAway(addr string, now time.Time) bool {
	tries.Lock()
	defer tries.Unlock()
	t := tries.by[addr]
	return t != nil && now.Sub(t.from) < failWindow && t.n >= failures
}

// failed counts a failure from an address.
func failed(addr string, now time.Time) {
	tries.Lock()
	defer tries.Unlock()
	if tries.by == nil {
		tries.by = map[string]*tally{}
	}
	t := tries.by[addr]
	if t == nil || now.Sub(t.from) >= failWindow {
		if len(tries.by) >= addressesKept {
			for k, old := range tries.by {
				if now.Sub(old.from) >= failWindow {
					delete(tries.by, k)
				}
			}
		}
		if len(tries.by) >= addressesKept {
			return
		}
		t = &tally{from: now}
		tries.by[addr] = t
	}
	t.n++
}
