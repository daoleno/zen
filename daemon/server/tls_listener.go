package server

import (
	"bufio"
	"crypto/tls"
	"net"
	"time"
)

type peekedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c peekedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

type tlsHTTPListener struct {
	net.Listener
	config *tls.Config
}

func (l *tlsHTTPListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	first, err := reader.Peek(1)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	inner := peekedConn{Conn: conn, reader: reader}
	_ = conn.SetDeadline(time.Time{})
	if first[0] == 0x16 {
		return tls.Server(inner, l.config), nil
	}
	return inner, nil
}
