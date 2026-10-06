//go:build unix

package pgwire

import (
	"io"
	"net"
	"os"
	"syscall"
)

// dedicate turns a connection into a thread-per-connection one, the way
// PostgreSQL serves each client from its own process: reads and writes are
// blocking system calls on the socket, so the kernel wakes the session the
// moment a message arrives, without a round trip through Go's network poller.
// The caller must lock the goroutine to its OS thread. It returns false if
// the connection cannot be dedicated.
func dedicate(c net.Conn) (net.Conn, bool) {
	fc, ok := c.(interface{ File() (*os.File, error) })
	if !ok {
		return c, false
	}
	f, err := fc.File() // a blocking duplicate of the socket
	if err != nil {
		return c, false
	}
	return &threadConn{Conn: c, f: f, fd: int(f.Fd())}, true
}

type threadConn struct {
	net.Conn
	f  *os.File
	fd int
}

func (t *threadConn) Read(p []byte) (int, error) {
	for {
		n, err := syscall.Read(t.fd, p)
		switch {
		case err == syscall.EINTR:
			continue
		case err != nil:
			return 0, err
		case n == 0 && len(p) > 0:
			return 0, io.EOF
		}
		return n, nil
	}
}

func (t *threadConn) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		n, err := syscall.Write(t.fd, p[written:])
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return written, err
		}
		written += n
	}
	return written, nil
}

func (t *threadConn) Close() error {
	_ = t.f.Close()
	return t.Conn.Close()
}
