// Package bridge relays bytes between one serial port and one TCP client at a time.
//
// It knows nothing about hardware: the serial side is whatever the Opener
// returns, so tests drive it with in-memory pipes.
package bridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"time"
)

// Opener blocks until a serial port is open and returns it, or returns ctx.Err()
// once ctx is done. It is called again after every serial failure.
type Opener func(ctx context.Context) (io.ReadWriteCloser, error)

// Flusher is implemented by ports that can discard pending input and output.
// The bridge flushes the port every time a new client connects.
type Flusher interface {
	Flush() error
}

// Config wires Run to the outside world.
type Config struct {
	Listener   net.Listener
	Open       Opener
	Logger     *slog.Logger
	RetryDelay time.Duration
}

const bufSize = 4096

// Run serves cfg.Listener until ctx is done, closing the listener before it
// returns. It returns nil on cancellation and the accept error if the
// listener fails for good.
//
// While no serial port is open, every accepted connection is closed at once so
// the client sees "not responding" rather than "connection refused". While a
// port is open, exactly one client is served; a newer connection replaces the
// current one.
func Run(ctx context.Context, cfg Config) error {
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	defer cfg.Listener.Close()

	conns := make(chan net.Conn)
	acceptErr := make(chan error, 1)
	go acceptLoop(ctx, cfg.Listener, conns, acceptErr)

	for {
		port, err := awaitPort(ctx, cfg.Open, conns, acceptErr)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, errAccept) {
				return errors.Unwrap(err)
			}
			log.Warn("could not open serial port", "err", err)
			if !sleep(ctx, cfg.RetryDelay) {
				return nil
			}
			continue
		}

		err = relay(ctx, log, port, conns, acceptErr)
		port.Close()
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errAccept) {
			return errors.Unwrap(err)
		}
		log.Warn("serial port lost", "err", err)
		if !sleep(ctx, cfg.RetryDelay) {
			return nil
		}
	}
}

// errAccept wraps a fatal listener error so Run can tell it from serial errors.
var errAccept = errors.New("bridge: listener failed")

type acceptError struct{ err error }

func (e acceptError) Error() string        { return errAccept.Error() + ": " + e.err.Error() }
func (e acceptError) Is(target error) bool { return target == errAccept }
func (e acceptError) Unwrap() error        { return e.err }

func acceptLoop(ctx context.Context, ln net.Listener, conns chan<- net.Conn, acceptErr chan<- error) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			acceptErr <- acceptError{err}
			return
		}
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
		}
		select {
		case conns <- c:
		case <-ctx.Done():
			c.Close()
			return
		}
	}
}

// awaitPort runs open and, until it returns, refuses every incoming connection.
func awaitPort(ctx context.Context, open Opener, conns <-chan net.Conn, acceptErr <-chan error) (io.ReadWriteCloser, error) {
	type result struct {
		port io.ReadWriteCloser
		err  error
	}
	res := make(chan result, 1)
	go func() {
		p, err := open(ctx)
		res <- result{p, err}
	}()
	for {
		select {
		case r := <-res:
			return r.port, r.err
		case c := <-conns:
			c.Close()
		case err := <-acceptErr:
			// The opener still owns its goroutine; let it finish on ctx.
			return nil, err
		case <-ctx.Done():
			r := <-res
			if r.port != nil {
				r.port.Close()
			}
			return nil, ctx.Err()
		}
	}
}

// client is the current TCP peer and the goroutine reading from it.
type client struct {
	conn  net.Conn
	data  chan []byte
	err   chan error
	done  chan struct{}
	first bool // no bytes received yet
}

func newClient(c net.Conn) *client {
	cl := &client{conn: c, data: make(chan []byte), err: make(chan error, 1), done: make(chan struct{}), first: true}
	go readInto(c, cl.data, cl.err, cl.done)
	return cl
}

func (cl *client) close() {
	close(cl.done)
	cl.conn.Close()
}

// readInto copies r into data until an error, which it reports on errc. It
// gives up on sending once done is closed.
func readInto(r io.Reader, data chan<- []byte, errc chan<- error, done <-chan struct{}) {
	for {
		buf := make([]byte, bufSize)
		n, err := r.Read(buf)
		if n > 0 {
			select {
			case data <- buf[:n]:
			case <-done:
				return
			}
		}
		if err != nil {
			errc <- err
			return
		}
	}
}

// relay serves clients through port until the port fails, ctx ends, or the
// listener dies. The caller closes port afterwards.
func relay(ctx context.Context, log *slog.Logger, port io.ReadWriteCloser, conns <-chan net.Conn, acceptErr <-chan error) error {
	serialData := make(chan []byte)
	serialErr := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go readInto(port, serialData, serialErr, done)

	var cur *client
	defer func() {
		if cur != nil {
			cur.close()
		}
	}()
	// nil channels block forever in select, which is exactly "no client".
	var curData chan []byte
	var curErr chan error

	for {
		select {
		case c := <-conns:
			if cur != nil {
				log.Debug("client replaced", "old", cur.conn.RemoteAddr(), "new", c.RemoteAddr())
				cur.close()
			} else {
				log.Debug("client connected", "addr", c.RemoteAddr())
			}
			if f, ok := port.(Flusher); ok {
				if err := f.Flush(); err != nil {
					c.Close()
					return err
				}
			}
			cur = newClient(c)
			curData, curErr = cur.data, cur.err

		case data := <-serialData:
			if cur == nil {
				continue // nobody is listening; drop it
			}
			if _, err := cur.conn.Write(data); err != nil {
				log.Debug("client write failed", "err", err)
				cur.close()
				cur, curData, curErr = nil, nil, nil
			}

		case data := <-curData:
			if cur.first {
				cur.first = false
				if looksLikeHTTP(data) {
					// A browser can be tricked into sending an HTTP request to
					// localhost:4030; never let that reach the mount.
					log.Warn("rejected client that speaks HTTP, not a telescope protocol", "addr", cur.conn.RemoteAddr())
					cur.close()
					cur, curData, curErr = nil, nil, nil
					continue
				}
			}
			if _, err := port.Write(data); err != nil {
				return err
			}

		case err := <-curErr:
			if !errors.Is(err, io.EOF) {
				log.Debug("client read failed", "err", err)
			} else {
				log.Debug("client disconnected", "addr", cur.conn.RemoteAddr())
			}
			cur.close()
			cur, curData, curErr = nil, nil, nil

		case err := <-serialErr:
			return err

		case err := <-acceptErr:
			return err

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// httpMethods are the standard request-line starts. Browsers may also send a
// custom method token through fetch(), so the HTTP version marker that every
// request line carries is checked as well. No telescope protocol sends a
// method-and-space or " HTTP/" at the start of a session.
var httpMethods = []string{"GET ", "POST ", "PUT ", "HEAD ", "DELETE ", "OPTIONS ", "PATCH ", "CONNECT ", "TRACE "}

// looksLikeHTTP reports whether the first bytes from a client form an HTTP
// request rather than mount traffic.
func looksLikeHTTP(first []byte) bool {
	for _, m := range httpMethods {
		if len(first) >= len(m) && string(first[:len(m)]) == m {
			return true
		}
	}
	return bytes.Contains(first, []byte(" HTTP/"))
}

// sleep waits for d or until ctx is done; it reports whether the full delay elapsed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
