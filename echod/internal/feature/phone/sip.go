package phone

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// registerFor is how long a registration lasts before it is renewed. Short enough that a device that
// went away stops being rung soon, and long enough not to be chatty.
const registerFor = 5 * time.Minute

// line is one signed-in SIP account: the user agent, its transport, and the calls through it.
type line struct {
	acct Account
	host string // the provider's host, Server without its port
	ua   *sipgo.UserAgent
	dg   *diago.Diago
	tran string // "tls" or "udp"
	port int
}

// providerCiphers are what the TLS connection to the provider may use. VoIP.ms offers only RSA key
// exchange (TLS_RSA_WITH_AES_256_GCM_SHA384), which Go leaves out unless it is asked for by name. It
// is still encrypted and the certificate is still checked; what it lacks is forward secrecy, which the
// provider decides. The ECDHE suites come first so a provider that offers them gets them.
var providerCiphers = []uint16{
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
}

// open signs nothing in yet: it builds the user agent and starts answering what arrives on it.
// incoming is called for every call offered, on its own goroutine, and the call lasts as long as it
// runs.
func open(ctx context.Context, acct Account, incoming func(*diago.DialogServerSession)) (*line, error) {
	l := &line{acct: acct, tran: "tls"}
	def := 5061
	if acct.Plain {
		l.tran, def = "udp", 5060
	}
	l.host, l.port = acct.hostPort(def)

	host, err := localAddr(l.host)
	if err != nil {
		return nil, err
	}

	opts := []sipgo.UserAgentOption{
		// The From user is what the provider matches the account on, so it is the SIP username.
		sipgo.WithUserAgent(acct.Username),
		// The From domain is the provider's: VoIP.ms takes the device's own address there, but
		// Linphone's server drops a REGISTER from an address it does not serve without a word.
		sipgo.WithUserAgentHostname(l.host),
		sipgo.WithUserAgenTLSConfig(&tls.Config{ServerName: l.host, CipherSuites: providerCiphers, MinVersion: tls.VersionTLS12}),
		sipgo.WithUserAgentTransportLayerOptions(sip.WithTransportLayerLogger(sipLogger()), sip.WithTransportLayerReadFilter(newFramer().filter)),
	}
	ua, err := sipgo.NewUA(opts...)
	if err != nil {
		return nil, err
	}
	l.ua = ua

	tr := diago.Transport{Transport: l.tran, BindHost: host}
	if !acct.Plain {
		// Calls arrive over the connection the registration keeps open, so nothing ever connects to
		// this listener; diago still starts one, and a TLS listener cannot start without a
		// certificate. A throwaway one, never shown to anyone.
		cert, err := throwawayCert()
		if err != nil {
			ua.Close()
			return nil, err
		}
		tr.TLSConf = &tls.Config{Certificates: []tls.Certificate{cert}}
		tr.MediaSRTP = 1 // SDES, which is what a provider offers alongside SIP over TLS
	}

	quiet := slog.New(slog.NewTextHandler(slogWriter{}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	l.dg = diago.NewDiago(ua, diago.WithTransport(tr), diago.WithLogger(quiet))
	if err := l.dg.ServeBackground(ctx, incoming); err != nil {
		ua.Close()
		return nil, fmt.Errorf("phone: listening: %w", err)
	}
	return l, nil
}

func (l *line) close() { l.ua.Close() }

func (l *line) uri(user string) (sip.Uri, error) {
	var u sip.Uri
	s := fmt.Sprintf("sip:%s@%s:%d", user, l.host, l.port)
	if l.tran != "udp" {
		s += ";transport=" + l.tran
	}
	err := sip.ParseUri(s, &u)
	return u, err
}

// register keeps the account signed in until ctx ends, and signs it out then. registered is called
// each time the provider accepts it.
func (l *line) register(ctx context.Context, registered func()) error {
	u, err := l.uri(l.acct.Username)
	if err != nil {
		return err
	}
	// RetryInterval is also how often diago registers again, which the firewalls rely on: a call to a
	// UDP account arrives on the registration's own conntrack entry, which lapses after three minutes.
	return l.dg.Register(ctx, u, diago.RegisterOptions{
		Username:      l.acct.Username,
		Password:      l.acct.Password,
		Expiry:        registerFor,
		RetryInterval: 30 * time.Second,
		OnRegistered:  registered,
	})
}

// dial places a call and returns once it is answered, refused, or ctx ends.
func (l *line) dial(ctx context.Context, number string) (*diago.DialogClientSession, error) {
	u, err := l.uri(number)
	if err != nil {
		return nil, err
	}
	return l.dg.Invite(ctx, u, diago.InviteOptions{
		Username:  l.acct.Username,
		Password:  l.acct.Password,
		Transport: l.tran,
	})
}

// withoutFeedback turns an offer of RTP/AVPF or RTP/SAVPF, as Linphone makes, into the plain
// profile diago answers. The feedback is only an extra: the answer leaves it out, and the caller goes
// without it.
func withoutFeedback(sdp []byte) []byte {
	lines := strings.Split(string(sdp), "\n")
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "m=") {
			continue
		}
		f := strings.Fields(strings.TrimRight(ln, "\r"))
		if len(f) < 3 || (f[2] != "RTP/AVPF" && f[2] != "RTP/SAVPF") {
			continue
		}
		lines[i] = strings.Replace(ln, " "+f[2]+" ", " "+strings.TrimSuffix(f[2], "F")+" ", 1)
	}
	return []byte(strings.Join(lines, "\n"))
}

// mediaAddress moves the audio stream's own c= line, when it has one, up to the session's, which is
// the only one diago reads. Linphone puts its relay there and the phone's own address at the session,
// so diago would send to the phone's address at the relay's port, where nothing listens.
func mediaAddress(sdp []byte) []byte {
	lines := strings.Split(string(sdp), "\n")
	session, media := -1, -1
	section := "" // the m= line the c= lines below belong to, "" for the session
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "m="):
			section = ln
		case !strings.HasPrefix(ln, "c="):
		case section == "" && session < 0:
			session = i
		case strings.HasPrefix(section, "m=audio ") && media < 0:
			media = i
		}
	}
	if session < 0 || media < 0 {
		return sdp
	}
	lines[session] = lines[media]
	return []byte(strings.Join(lines, "\n"))
}

// localAddr is the address this device reaches the provider from, which is what goes into the call's
// media description. The provider sees past it (the home router's address), but it has to be one the
// device really has.
func localAddr(server string) (string, error) {
	c, err := net.Dial("udp", net.JoinHostPort(server, "5060"))
	if err != nil {
		return "", fmt.Errorf("phone: reaching %s: %w", server, err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

func throwawayCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// slogWriter sends the library's own warnings to the daemon's log, one line each.
type slogWriter struct{}

func (slogWriter) Write(b []byte) (int, error) {
	slog.Warn("phone: sip", "said", strings.TrimSpace(string(b)))
	return len(b), nil
}
