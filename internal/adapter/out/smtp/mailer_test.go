package smtpout_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	smtpout "github.com/code-corhuila/multi-tour-worker-notifications/internal/adapter/out/smtp"
	"github.com/code-corhuila/multi-tour-worker-notifications/internal/application/port/out"
)

// fakeServer is a small SMTP server that records what it is given. It can offer STARTTLS and can refuse a recipient.
type fakeServer struct {
	ln          net.Listener
	tlsConfig   *tls.Config // nil: STARTTLS is not offered
	rejectRcpt  bool
	mu          sync.Mutex
	mailFrom    string
	rcptTo      string
	data        string
	startedTLS  bool
	commandSeen []string
}

func startServer(t *testing.T, tlsConfig *tls.Config, rejectRcpt bool) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{ln: ln, tlsConfig: tlsConfig, rejectRcpt: rejectRcpt}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeServer) record(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

func (s *fakeServer) serve(conn net.Conn) {
	defer conn.Close()
	r := textproto.NewReader(bufio.NewReader(conn))
	say := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	say("220 fake ESMTP")
	for {
		line, err := r.ReadLine()
		if err != nil {
			return
		}
		cmd := strings.ToUpper(line)
		s.record(func() { s.commandSeen = append(s.commandSeen, strings.Fields(cmd + " ")[0]) })
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if s.tlsConfig != nil && !s.started() {
				say("250-fake")
				say("250 STARTTLS")
			} else {
				say("250 fake")
			}
		case cmd == "STARTTLS":
			say("220 ready")
			conn = tls.Server(conn, s.tlsConfig)
			r = textproto.NewReader(bufio.NewReader(conn))
			say = func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
			s.record(func() { s.startedTLS = true })
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.record(func() { s.mailFrom = strings.Trim(line[len("MAIL FROM:"):], "<> ") })
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			if s.rejectRcpt {
				say("550 no such user")
				continue
			}
			s.record(func() { s.rcptTo = strings.Trim(line[len("RCPT TO:"):], "<> ") })
			say("250 ok")
		case cmd == "DATA":
			say("354 go on")
			body, err := r.ReadDotBytes()
			if err != nil {
				return
			}
			s.record(func() { s.data = string(body) })
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (s *fakeServer) started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startedTLS
}

func selfSigned(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}, pool
}

func message() out.Message {
	return out.Message{FromName: "Travesía Natural", To: "owner@acme.test", Subject: "Se restableció la contraseña de tu cuenta",
		HTMLBody: "<p>Hola, la contraseña de tu cuenta se restableció.</p>"}
}

func newMailer(port int, requireTLS bool, tlsConfig *tls.Config) *smtpout.Mailer {
	return smtpout.New(smtpout.Config{Host: "127.0.0.1", Port: port, From: "no-reply@multitour.local", RequireTLS: requireTLS,
		Timeout: 3 * time.Second, TLSConfig: tlsConfig})
}

// AC-WK4-01.1: the envelope goes to the one recipient from the platform address, and the message is well formed.
func TestC_the_message_reaches_the_server_with_the_tenant_name_as_visible_sender(t *testing.T) {
	srv := startServer(t, nil, false)
	if err := newMailer(srv.port(), false, nil).Send(context.Background(), message()); err != nil {
		t.Fatal(err)
	}
	if srv.mailFrom != "no-reply@multitour.local" || srv.rcptTo != "owner@acme.test" {
		t.Fatalf("envelope from %q to %q", srv.mailFrom, srv.rcptTo)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(srv.data))
	if err != nil {
		t.Fatalf("the data is not a message: %v", err)
	}
	from, err := parsed.Header.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Name != "Travesía Natural" || from[0].Address != "no-reply@multitour.local" {
		t.Fatalf("From %v, err %v", from, err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != message().Subject {
		t.Fatalf("subject %q, err %v", subject, err)
	}
	if ct := parsed.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") || !strings.Contains(strings.ToLower(ct), "utf-8") {
		t.Fatalf("content type %q", ct)
	}
	body, _ := io.ReadAll(parsed.Body)
	if len(body) == 0 || strings.Contains(string(body), "<p>") {
		t.Fatalf("the body should be transfer-encoded, got %q", body)
	}
}

// AC-WK4-01.2 and RN-WK4-01: a server that refuses the recipient is an error, so the route answers 5xx.
func TestC_a_refused_recipient_is_an_error(t *testing.T) {
	srv := startServer(t, nil, true)
	if err := newMailer(srv.port(), false, nil).Send(context.Background(), message()); err == nil {
		t.Fatal("a refused recipient was reported as sent")
	}
	if srv.data != "" {
		t.Fatal("data was sent after the recipient was refused")
	}
}

func TestC_an_unreachable_server_is_an_error_within_the_timeout(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	start := time.Now()
	if err := newMailer(port, false, nil).Send(context.Background(), message()); err == nil {
		t.Fatal("an unreachable server was reported as sent")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the send ignored the timeout")
	}
}

// AC-WK4-01.8: outside development the connection must be TLS; a server that does not offer it gets nothing.
func TestU_tls_required_outside_development(t *testing.T) {
	srv := startServer(t, nil, false)
	if err := newMailer(srv.port(), true, nil).Send(context.Background(), message()); err == nil {
		t.Fatal("a plain connection was accepted although TLS is required")
	}
	if srv.mailFrom != "" || srv.data != "" {
		t.Fatal("the message went out in the clear")
	}
}

func TestC_starttls_is_used_when_required_and_the_certificate_is_checked(t *testing.T) {
	serverTLS, pool := selfSigned(t)
	srv := startServer(t, serverTLS, false)
	trusting := &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS12}
	if err := newMailer(srv.port(), true, trusting).Send(context.Background(), message()); err != nil {
		t.Fatal(err)
	}
	if !srv.started() || srv.rcptTo != "owner@acme.test" {
		t.Fatalf("starttls %v, recipient %q", srv.started(), srv.rcptTo)
	}
	// A certificate nobody trusts is refused: the default configuration checks the chain.
	other := startServer(t, serverTLS, false)
	if err := newMailer(other.port(), true, nil).Send(context.Background(), message()); err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
}

// I·mailpit_receives_the_notice: runs only where a Mailpit is given (the CI starts one and sets both variables).
func TestI_mailpit_receives_the_notice(t *testing.T) {
	smtpAddr, api := os.Getenv("MAILPIT_SMTP"), os.Getenv("MAILPIT_URL")
	if smtpAddr == "" || api == "" {
		t.Skip("MAILPIT_SMTP and MAILPIT_URL are not set")
	}
	host, portText, _ := net.SplitHostPort(smtpAddr)
	port, _ := strconv.Atoi(portText)
	m := smtpout.New(smtpout.Config{Host: host, Port: port, From: "no-reply@multitour.local", Timeout: 5 * time.Second})
	msg := message()
	msg.To = "mailpit-check@acme.test"
	if err := m.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(api + "/api/v1/search?query=" + "to:mailpit-check@acme.test")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var found struct {
		Total    int `json:"messages_count"`
		Messages []struct {
			Subject string `json:"Subject"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(res.Body).Decode(&found); err != nil || found.Total < 1 || found.Messages[0].Subject != msg.Subject {
		t.Fatalf("Mailpit did not capture the notice: %+v, err %v", found, err)
	}
}
