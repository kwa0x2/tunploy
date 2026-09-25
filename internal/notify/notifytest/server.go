// Package notifytest runs a minimal plain-text SMTP server for tests.
package notifytest

import (
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type Mail struct {
	From string
	To   []string
	Data string
}

type Server struct {
	Host string
	Port int
	// Recipients the server refuses, to test failures.
	Reject []string

	mu    sync.Mutex
	mails []Mail
}

func New(t testing.TB) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	addr := ln.Addr().(*net.TCPAddr)
	s := &Server{Host: "127.0.0.1", Port: addr.Port}
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

func (s *Server) Mails() []Mail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mail(nil), s.mails...)
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	tp := textproto.NewConn(conn)
	reply := func(lines ...string) { tp.PrintfLine("%s", strings.Join(lines, "\r\n")) }
	reply("220 fake ESMTP")

	var cur Mail
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			reply("250-fake", "250 8BITMIME")
		case "MAIL":
			cur = Mail{From: address(arg)}
			reply("250 ok")
		case "RCPT":
			to := address(arg)
			if s.rejects(to) {
				reply("550 5.1.1 no such user " + to)
				continue
			}
			cur.To = append(cur.To, to)
			reply("250 ok")
		case "DATA":
			reply("354 go ahead")
			data, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			cur.Data = string(data)
			s.mu.Lock()
			s.mails = append(s.mails, cur)
			s.mu.Unlock()
			reply("250 queued as " + strconv.Itoa(len(s.Mails())))
		case "RSET", "NOOP":
			reply("250 ok")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

func (s *Server) rejects(to string) bool {
	for _, r := range s.Reject {
		if r == to {
			return true
		}
	}
	return false
}

func address(arg string) string {
	_, rest, _ := strings.Cut(arg, ":")
	rest = strings.TrimSpace(rest)
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	return strings.Trim(rest, "<>")
}
