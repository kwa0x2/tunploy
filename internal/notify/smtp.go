// Package notify emails the panel's activity events to the admin.
package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

const (
	SecurityStartTLS = "starttls"
	SecurityTLS      = "tls"
	SecurityNone     = "none"

	smtpTimeout = 30 * time.Second
)

type SMTP struct {
	Host     string
	Port     int
	Security string
	Username string
	Password string
	From     string
}

type Message struct {
	To      []string
	Subject string
	Body    string
}

// Send delivers one message, failing with the server's own words where it can.
func (c SMTP) Send(ctx context.Context, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, smtpTimeout)
	defer cancel()

	from, err := mail.ParseAddress(c.From)
	if err != nil {
		return fmt.Errorf("from address: %w", err)
	}
	body, err := render(from, m, time.Now())
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	tlsConfig := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	if c.Security == SecurityTLS {
		conn, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("greeting from %s: %w", addr, err)
	}
	defer client.Close()

	if c.Security == SecurityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS; choose TLS for port 465, or None")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if c.Username != "" {
		// PlainAuth itself refuses to send the password over an unencrypted link.
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("sign in: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("sender %s: %w", from.Address, err)
	}
	for _, to := range m.To {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("recipient %s: %w", to, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("data: %w", err)
	}
	return client.Quit()
}

func render(from *mail.Address, m Message, now time.Time) ([]byte, error) {
	if from.Name == "" {
		from = &mail.Address{Name: "Tunploy", Address: from.Address}
	}
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	id := make([]byte, 12)
	rand.Read(id)

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", strings.Join(m.To, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&b)
	body := strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := qp.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
