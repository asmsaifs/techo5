package nearest

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// How a claim proves it comes from the house: an HMAC keyed from the house word, over when it was sent, a
// nonce, the device it is for and the claim (which carries its turn), the way announcements are signed
// (feature/announce) but with a key of its own, so a signature of one can never pass for the other.
// Without it anything on the network could stop every device in the house answering, by claiming every
// wake word louder than all of them. Naming the device it is for keeps a claim seen on its way to one
// device from being sent on to another.

const (
	authHeader = "X-Techo5-Nearest"

	// version is the claim's form. v2 keys it from the house word slowly (key), so a claim heard on the
	// network is no quick way to guess the word: v1 claims, keyed in one hash, are not taken any more,
	// and a device still sending them is answered as though nothing claimed (it answers too).
	version = "v2"

	// keySalt and keyRounds are the slow keying. The salt is this protocol's own, so the key is no use to
	// announcements or the intercom, which sign with the same word.
	keySalt   = "techo5-nearest v2"
	keyRounds = 100_000

	// skew is how far a sender's clock may be from this one's. The devices keep time from the network;
	// a claim is only of use for a fraction of a second, so a replay is what this is about.
	skew = 5 * time.Second

	// noncesKept bounds the nonces remembered. A house makes a claim per device per wake word.
	noncesKept = 1024

	// maxBody is more than any claim.
	maxBody = 1 << 12
)

// keys is the key of the house word in use, worked out once: it takes a moment by design.
var keys struct {
	sync.Mutex
	word string
	key  []byte
}

// key is the HMAC key for a house word: PBKDF2 over it, so guessing the word from a claim costs as much
// per guess as working the key out does here.
func key(word string) []byte {
	keys.Lock()
	defer keys.Unlock()
	if keys.key != nil && keys.word == word {
		return keys.key
	}
	k, err := pbkdf2.Key(sha256.New, word, []byte(keySalt), keyRounds, sha256.Size)
	if err != nil {
		k = nil // cannot happen for these sizes; a nil key signs nothing another device checks
	}
	keys.word, keys.key = word, k
	return k
}

// warm works the key out ahead of the first wake word, so that one is not held up by it.
func warm(word string) {
	if word != "" {
		go key(word)
	}
}

// mac is the signature over a claim to the device called to. The name is compared without regard to
// case, as the devices find each other.
func mac(word, to, ts, nonce string, body []byte) []byte {
	sum := sha256.Sum256(body)
	m := hmac.New(sha256.New, key(word))
	m.Write([]byte(version + "\n" + ts + "\n" + nonce + "\n" + strings.ToLower(to) + "\n" + hex.EncodeToString(sum[:]) + "\n"))
	return m.Sum(nil)
}

// sign is the header for a claim about to be sent to the device called to.
func sign(word, to string, body []byte, now time.Time) string {
	var n [12]byte
	_, _ = rand.Read(n[:])
	ts, nonce := strconv.FormatInt(now.Unix(), 10), hex.EncodeToString(n[:])
	return version + " " + ts + " " + nonce + " " + hex.EncodeToString(mac(word, to, ts, nonce, body))
}

// started is when this program started. A claim signed before it is refused, so one taken after a
// restart, while the nonces it remembers are gone, cannot be sent again.
var started = time.Now()

// startedWall is when this program started, by the wall clock as it reads at now: worked out from the
// monotonic clock, so a clock set back after the start (by NTP, say) does not put the start ahead of
// every claim until the time catches up.
func startedWall(now time.Time) time.Time { return now.Add(-time.Since(started)).Truncate(time.Second) }

// looksSigned checks a claim's header before its body is read: its form, and a time this device would
// take. A request that fails it is turned away without waiting on anything it sends.
func looksSigned(header string, now time.Time) error {
	parts := strings.Fields(header)
	if len(parts) != 4 || parts[0] != version || len(parts[3]) != 2*sha256.Size {
		return errUnsigned
	}
	sec, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return errUnsigned
	}
	at := time.Unix(sec, 0)
	if d := now.Sub(at); d > skew || d < -skew || at.Before(startedWall(now)) {
		return errStale
	}
	return nil
}

var (
	errUnsigned = errors.New("not signed by this house")
	errStale    = errors.New("signed at a time too far from this device's clock")
	errReplayed = errors.New("a claim already taken")
)

// verify checks a claim's header against the house word, as a claim for the device called me.
func verify(word, me, header string, body []byte, now time.Time) error {
	if word == "" {
		return errUnsigned
	}
	if err := looksSigned(header, now); err != nil {
		return err
	}
	parts := strings.Fields(header)
	got, err := hex.DecodeString(parts[3])
	if err != nil || !hmac.Equal(got, mac(word, me, parts[1], parts[2], body)) {
		return errUnsigned
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

// fresh is whether a nonce has not been seen within the skew, remembering it.
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

func readBody(r *http.Request) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBody {
		return nil, errors.New("too long for a claim")
	}
	return b, nil
}

// answeredHeader proves a refusal comes from a device of this house. Devices are found over mDNS, which
// anything on the network can answer, so a refusal saying "answered here already" is only believed with
// this on it: otherwise a host that knows nothing could stand every device in the house down.
const answeredHeader = "X-Techo5-Nearest-Answered"

// answeredMAC is that proof: over the refused claim's nonce, the device that sent it and the device that
// answered.
func answeredMAC(word, nonce, from, by string) string {
	m := hmac.New(sha256.New, key(word))
	m.Write([]byte("answered\n" + nonce + "\n" + strings.ToLower(from) + "\n" + strings.ToLower(by) + "\n"))
	return hex.EncodeToString(m.Sum(nil))
}

// nonceOf is the nonce in a claim's signature header, empty for none.
func nonceOf(header string) string {
	if parts := strings.Fields(header); len(parts) == 4 {
		return parts[2]
	}
	return ""
}

// answeredBy is whether a refusal's proof checks out, for a claim signed with header, sent by from to by.
func answeredBy(word, header, proof, from, by string) bool {
	nonce := nonceOf(header)
	if word == "" || nonce == "" || proof == "" {
		return false
	}
	return hmac.Equal([]byte(proof), []byte(answeredMAC(word, nonce, from, by)))
}
