package connection

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net"
	"time"
)

// TCPConnection is an open connection over TCP.
type TCPConnection struct {
	socket         net.Conn
	hname          string
	doneSignal     chan struct{}
	closeInitiated bool
	closed         bool
	log            LoggingCallbacks
	recvHandler    ReceiveHandler
}

// OpenTCPConnection opens a new TCP connection, optionally with SSL enabled.
func OpenTCPConnection(recvHandler ReceiveHandler, logCBs LoggingCallbacks, host net.IP, port int, opts Options) (*TCPConnection, error) {
	// ensure user did not maually create loggingcallbacks
	if !logCBs.isValid() {
		return nil, fmt.Errorf("uninitialized LoggingCallbacks passed to connection.OpenTCPConnection() call; was it obtained using connection.NewLoggingCallbacks()?")
	}

	if recvHandler == nil {
		return nil, fmt.Errorf("recvHandler must be provided for output delivery")
	}

	hostSocketAddr := fmt.Sprintf("%s:%d", host, port)

	conn := &TCPConnection{
		doneSignal:  make(chan struct{}),
		log:         logCBs,
		hname:       hostSocketAddr,
		recvHandler: recvHandler,
	}

	dialer := &net.Dialer{}
	if opts.ConnectionTimeout > 0 {
		dialer.Timeout = opts.ConnectionTimeout
	}
	if opts.DisableKeepalives {
		dialer.KeepAlive = -1 * time.Second
	}

	if opts.TLSEnabled {
		tlsConf := &tls.Config{
			InsecureSkipVerify: opts.TLSSkipVerify,
		}

		if opts.TLSTrustChain != "" {
			certs, err := ioutil.ReadFile(opts.TLSTrustChain)
			if err != nil {
				return nil, fmt.Errorf("could not read trust chain: %v", err)
			}

			rootCAs, err := x509.SystemCertPool()
			if err != nil {
				rootCAs = x509.NewCertPool()
			}

			if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
				return nil, fmt.Errorf("could not parse any valid certificate authorities from trust chain file")
			}
			tlsConf.RootCAs = rootCAs
		}

		var err error
		conn.socket, err = tls.DialWithDialer(dialer, "tcp", hostSocketAddr, tlsConf)
		if err != nil {
			return conn, err
		}
	} else {
		var err error
		conn.socket, err = dialer.Dial("tcp", hostSocketAddr)
		if err != nil {
			return conn, err
		}
	}

	// start reader thread
	go func() {
		defer close(conn.doneSignal)
		defer func() { conn.closed = true }()

		buf := make([]byte, readerBufferSize)

		for {
			// non-blocking read so we can check if we've been instructed to shut down
			n, err := conn.socket.Read(buf)

			if n > 0 {
				dataBytes := make([]byte, n)
				copy(dataBytes, buf[:n])

				// excecute reveive handler in go routine for 2 reasons
				// 1. allows us to continue checking for more bytes quickly
				// 2. recvHandler exploding won't kill all future attempts to
				// pass to recvHandler.
				go func() {
					conn.log.traceCb("received bytes %s", hex.EncodeToString(dataBytes))
					conn.recvHandler(dataBytes)
				}()
			}
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					if !conn.closeInitiated {
						conn.log.errorCb(err, "socket closed unexpectedly: %v", err)
					}
					// we hit a deadline. immediately exit due to requested exit.
				} else if conn.closeInitiated {
					conn.log.errorCb(err, "while closing, got non-close error: %v", err)
				} else {
					conn.log.errorCb(err, "socket error: %v", err)
					conn.socket.Close()
				}
				break
			}
		}
	}()

	// if we're in TCP connection there is no excuse for not checking
	// that this is a valid connection; in the (moderately common case) of
	// connecting to a docker port, if docker is up but the service in container
	// isn't it will instantly drop an accepted TCP connection. Detect that
	// by waiting a small amount of time for disconnect to be receieved.
	//
	// ofc, anything with a ping time of >100 will still be returned as not
	// invalid, but that's okay, it'll be detected later and this at least
	// improves the fail fast for some cases.
	time.Sleep(100 * time.Millisecond)
	if conn.IsClosed() {
		return conn, fmt.Errorf("host accepted connection but immediately closed it")
	}

	return conn, nil
}

// IsClosed checks if the connection has been closed
func (conn *TCPConnection) IsClosed() bool {
	return conn.closed
}

// Close shuts down the connection contained in the given object. Waits maximum 5 seconds after sending close before assuming that the close was successful.
// After the connection has been closed, it cannot be used to send any more messages.
func (conn *TCPConnection) Close() error {
	if conn.closed {
		return nil // it's already been closed
	}
	var err error
	conn.closeInitiated = true
	conn.socket.SetDeadline(time.Now().Add(50 * time.Millisecond))
	select {
	case <-conn.doneSignal:
	case <-time.After(5 * time.Second):
		conn.log.warnCb("clean close timed out after 5 seconds; forcing unclean close")
	}

	err = conn.socket.Close()
	// reader thread exiting due to the socket.Close() should also set
	// conn.closed = true but also set it here
	// so that future callers instantly can no longer perform operations on this connection
	conn.closed = true
	if err != nil {
		err = fmt.Errorf("error while closing connection: %v", err)
	}
	return err
}

// Send sends binary data over the connection. A response is not waited for, though depending on the
// connection a non-nil error indicates that a message was received (as is the case in TCP with an
// ACK in response to a client PSH.)
func (conn *TCPConnection) Send(data []byte) error {
	if conn.closed {
		return fmt.Errorf("this connection has been closed and can no longer be used to send")
	}
	n, err := conn.socket.Write(data)
	if err != nil {
		return fmt.Errorf("After writing %d byte(s), got error in write: %v", n, err)
	}

	return nil
}

// GetRemoteName returns the host that was connected to
func (conn *TCPConnection) GetRemoteName() string {
	return conn.hname
}

// GetLocalName returns the name of the local side of the connection.
func (conn *TCPConnection) GetLocalName() string {
	return conn.socket.LocalAddr().String()
}
