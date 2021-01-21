package connection

import (
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"time"
)

// UDPConnection is an open connection over UDP.
type UDPConnection struct {
	socket         net.Conn
	hname          string
	doneSignal     chan struct{}
	closeInitiated bool
	closed         bool
	log            LoggingCallbacks
	recvHandler    ReceiveHandler
}

// OpenUDPConnection opens a new UDP connection. SSL (DTLS) is not supported at this time.
func OpenUDPConnection(recvHandler ReceiveHandler, logCBs LoggingCallbacks, host net.IP, port int, opts Options) (*UDPConnection, error) {
	// ensure user did not maually create loggingcallbacks
	if !logCBs.isValid() {
		return nil, fmt.Errorf("uninitialized LoggingCallbacks passed to connection.OpenUDPConnection() call; was it obtained using connection.NewLoggingCallbacks()?")
	}

	if recvHandler == nil {
		return nil, fmt.Errorf("recvHandler must be provided for output delivery")
	}

	hostSocketAddr := fmt.Sprintf("%s:%d", host, port)

	conn := &UDPConnection{
		doneSignal:  make(chan struct{}),
		log:         logCBs,
		hname:       hostSocketAddr,
		recvHandler: recvHandler,
	}

	dialer := &net.Dialer{}
	if opts.ConnectionTimeout > 0 {
		dialer.Timeout = opts.ConnectionTimeout
	}

	if opts.TLSEnabled {
		return conn, fmt.Errorf("TLS over UDP (DTLS) is not supported")
	}

	var err error
	conn.socket, err = dialer.Dial("udp", hostSocketAddr)
	if err != nil {
		return conn, err
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
						conn.log.errorCb(err, "%v", err)
					}
					// we hit a deadline. immediately exit due to requested exit.
				} else if err != io.EOF {
					conn.log.errorCb(err, "socket error: %v", err)
				}
				conn.socket.Close()
				break
			}
		}
	}()

	return conn, nil
}

// IsClosed checks if the connection has been closed
func (conn *UDPConnection) IsClosed() bool {
	return conn.closed
}

// Close shuts down the UDP connection and frees the associated resources
func (conn *UDPConnection) Close() error {
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

// Send sends binary data over the connection. A response is not waited for.
func (conn *UDPConnection) Send(data []byte) error {
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
func (conn *UDPConnection) GetRemoteName() string {
	return conn.hname
}

// GetLocalName returns the name of the local side of the connection.
func (conn *UDPConnection) GetLocalName() string {
	return conn.socket.LocalAddr().String()
}
