package driver

import (
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"time"
)

// UDPConnection is an open connection over UDP.
type UDPConnection struct {
	socket          *net.UDPConn
	startedHalfOpen bool
	firstConnected  *net.UDPAddr
	hname           string
	doneSignal      chan struct{}
	closeInitiated  bool
	closed          bool
	log             LoggingCallbacks
	recvHandler     ReceiveHandler
}

// OpenUDPConnection opens a new UDP connection. SSL (DTLS) is not supported at this time.
func OpenUDPConnection(recvHandler ReceiveHandler, logCBs LoggingCallbacks, remoteHost string, remotePort int, bindAddr string, localPort int, opts Options) (*UDPConnection, error) {
	// ensure user did not maually create loggingcallbacks
	if !logCBs.isValid() {
		return nil, fmt.Errorf("uninitialized LoggingCallbacks passed to connection.OpenUDPConnection() call; was it obtained using connection.NewLoggingCallbacks()?")
	}

	if recvHandler == nil {
		return nil, fmt.Errorf("recvHandler must be provided for output delivery")
	}

	if (remoteHost == "" && remotePort > 0) || (remoteHost != "" && remotePort < 1) {
		return nil, fmt.Errorf("must give both remoteHost and remotePort if either is given")
	}

	if opts.TLSEnabled {
		return nil, fmt.Errorf("TLS over UDP (DTLS) is not supported")
	}

	var localSockAddr net.UDPAddr
	if bindAddr != "" || localPort > 0 {
		if bindAddr != "" {
			ip, err := resolveHost(bindAddr)
			if err != nil {
				return nil, err
			}
			localSockAddr.IP = ip
		}
		if localPort > 0 {
			localSockAddr.Port = localPort
		}
	}

	conn := &UDPConnection{
		doneSignal:  make(chan struct{}),
		log:         logCBs,
		recvHandler: recvHandler,
	}

	var err error
	if remoteHost == "" {
		if localSockAddr.Port == 0 {
			return nil, fmt.Errorf("need to provide a local port to listen on if not giving a remote host")
		}

		// this sock is going up in listener mode
		conn.startedHalfOpen = true
		conn.socket, err = net.ListenUDP("udp", &localSockAddr)
		if err != nil {
			return nil, fmt.Errorf("could not listen for connections: %v", err)
		}
	} else {
		hostSocketAddr := fmt.Sprintf("%s:%d", remoteHost, remotePort)
		conn.hname = hostSocketAddr

		dialer := &net.Dialer{}

		if bindAddr != "" || localPort > 0 {
			dialer.LocalAddr = &localSockAddr
		}
		if opts.ConnectionTimeout > 0 {
			dialer.Timeout = opts.ConnectionTimeout
		}

		netConn, err := dialer.Dial("udp", hostSocketAddr)
		if err != nil {
			return conn, err
		}

		var ok bool
		if conn.socket, ok = netConn.(*net.UDPConn); !ok {
			return nil, fmt.Errorf("did not get a UDP connection from dial")
		}
	}

	// start reader thread
	conn.startReaderThread()

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
	if !conn.Ready() {
		return fmt.Errorf("this connection doesn't yet have a remote host to communicate with")
	}

	var n int
	var err error
	if conn.startedHalfOpen {
		n, err = conn.socket.WriteToUDP(data, conn.firstConnected)
	} else {
		n, err = conn.socket.Write(data)
	}
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

// Ready returns whether a remote host is known. This will be true after the
// first remote host connects when none is provided at creation; if one is provided, this is
// instantly true.
func (conn *UDPConnection) Ready() bool {
	if conn.startedHalfOpen {
		return conn.firstConnected != nil
	}
	return true
}

func (conn *UDPConnection) startReaderThread() {
	go func() {
		defer close(conn.doneSignal)
		defer func() { conn.closed = true }()

		buf := make([]byte, readerBufferSize)

		for {
			var n int
			var err error
			if conn.startedHalfOpen {
				var remoteAddr *net.UDPAddr

				n, remoteAddr, err = conn.socket.ReadFromUDP(buf)
				if conn.firstConnected == nil {
					conn.log.debugCb("first client has connected from %v", remoteAddr)
					conn.firstConnected = remoteAddr
					conn.hname = conn.firstConnected.String()
				}

				if !conn.firstConnected.IP.Equal(remoteAddr.IP) || conn.firstConnected.Zone != remoteAddr.Zone || conn.firstConnected.Port != remoteAddr.Port {
					conn.log.debugCb("rejected data from non-first client %v", remoteAddr)
					// need to do an error check in case the sock just died.
					if err != nil {
						conn.handleSockError(err)
						break
					}
					continue
				}
			} else {
				n, err = conn.socket.Read(buf)
			}

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
				conn.handleSockError(err)
				break
			}
		}
	}()
}

func (conn *UDPConnection) handleSockError(err error) {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		if !conn.closeInitiated {
			conn.log.errorCb(err, "%v", err)
		}
		// we hit a deadline. immediately exit due to requested exit.
	} else if err != io.EOF {
		conn.log.errorCb(err, "socket error: %v", err)
	}
	conn.socket.Close()
}
