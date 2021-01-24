package driver

import (
	"crypto/tls"
	"crypto/x509"
	"dekarrin/netkarkat/internal/certs"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// TCPServerConnection is an open connection listening for a client to establish connection.
// On an establish, this will instantly convert its behavior to be that of the TCPConnection
// and will immediately stop listening for new establishes.
type TCPServerConnection struct {
	listener       *net.TCPListener
	listening      bool
	log            LoggingCallbacks
	doneSignal     chan struct{}
	closeInitiated bool
	closed         bool

	// estab is used by multiple go routines. all access must be synched via estabMutex.
	estab           *TCPConnection
	estabMutex      sync.Mutex
	estabClientAddr net.Addr

	timeout  time.Duration
	timedOut bool

	// we may need to cary the timeout to TLS handshaking; if so, this value
	// will be required
	listenStartTime time.Time

	keepAlives bool
	tlsConf    *tls.Config
	onRecv     ReceiveHandler
	onConnect  ClientConnectedHandler
}

// OpenTCPServer opens a new TCP server listening on the given port, bound to the given address. It will accept one and only one connection,
// at which point the returned connection will begin acting functionally like a TCPClientConnection to the connected host.
//
// Once a connection has been established, the server will begin accepting only connections from that
// remote socket address, including the same port. It will not accept any new connection until the
// current one has ended.
func OpenTCPServer(recvHandler ReceiveHandler, newClientHandler ClientConnectedHandler, logCBs LoggingCallbacks, bindAddr string, port int, opts Options) (*TCPServerConnection, error) {
	// ensure user did not maually create loggingcallbacks
	if !logCBs.isValid() {
		return nil, fmt.Errorf("uninitialized LoggingCallbacks passed to connection.OpenTCPServer() call; was it obtained using connection.NewLoggingCallbacks()?")
	}

	if recvHandler == nil {
		return nil, fmt.Errorf("recvHandler must be provided for output delivery")
	}
	if newClientHandler == nil {
		// this is okay, we'll just use a default. it's possible that caller does not care about
		// new clients.
		newClientHandler = func(string) {}
	}

	listenAddr := &net.TCPAddr{}
	if bindAddr != "" {
		ip, err := resolveHost(bindAddr)
		if err != nil {
			return nil, err
		}
		listenAddr.IP = ip
	}
	if port > 0 {
		listenAddr.Port = port
	}

	conn := &TCPServerConnection{
		doneSignal: make(chan struct{}),
		log:        logCBs,
		onRecv:     recvHandler,
		onConnect:  newClientHandler,
		keepAlives: !opts.DisableKeepalives,
		timeout:    opts.ConnectionTimeout,
	}

	if opts.TLSEnabled {
		tlsConf := &tls.Config{}
		if opts.TLSServerCertFile != "" && opts.TLSServerKeyFile != "" {
			keyPair, err := tls.LoadX509KeyPair(opts.TLSServerCertFile, opts.TLSServerKeyFile)
			if err != nil {
				return nil, err
			}
			tlsConf.Certificates = []tls.Certificate{keyPair}
		} else {
			// no certs were provided but ssl was requested. Generate our own.
			serverCert, caPEM, err := certs.GenerateSelfSignedTLSServerCertificate(opts.TLSServerCertCommonName, opts.TLSServerCertIPs)
			if err != nil {
				return nil, err
			}
			tlsConf.Certificates = []tls.Certificate{serverCert}

			caFilename := strings.ReplaceAll(fmt.Sprintf("netkk-ca-%s.pem", time.Now().Format(time.RFC3339)), ":", "-")
			err = ioutil.WriteFile(caFilename, caPEM, os.FileMode(0667))
			if err != nil {
				// if we cant write the ca it's not THAT bad; it's just that there will be no way to specify
				// to clients that the server cert's ca is to be trusted.
				logCBs.warnCb("could not write generated CA cert for self-signed cert: %v", err)
			}
			fmt.Printf("Wrote self-signed CA to %q\n", caFilename)

			// probably should trust own CA
			rootCAs, err := x509.SystemCertPool()
			if err != nil {
				rootCAs = x509.NewCertPool()
			}

			if ok := rootCAs.AppendCertsFromPEM(caPEM); !ok {
				return nil, fmt.Errorf("problem parsing generated CA PEM data")
			}
			tlsConf.RootCAs = rootCAs
		}

		if opts.TLSTrustChain != "" {
			certs, err := ioutil.ReadFile(opts.TLSTrustChain)
			if err != nil {
				return nil, fmt.Errorf("could not read trust chain: %v", err)
			}

			clientCAs, err := x509.SystemCertPool()
			if err != nil {
				clientCAs = x509.NewCertPool()
			}

			if ok := clientCAs.AppendCertsFromPEM(certs); !ok {
				return nil, fmt.Errorf("could not parse any valid certificate authorities from trust chain file")
			}
			tlsConf.ClientCAs = clientCAs
		}

		conn.tlsConf = tlsConf
	}

	var err error
	conn.listener, err = net.ListenTCP("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("could not listen for connections: %v", err)
	}

	// start accept thread
	conn.startListening()

	return conn, nil
}

// IsClosed checks if the connection has been closed.
func (conn *TCPServerConnection) IsClosed() bool {
	return conn.closed
}

// CloseActive shuts down only the active client connection.
func (conn *TCPServerConnection) CloseActive() error {
	var err error
	if err = conn.synchedInvalidateEstab(); err != nil {
		err = fmt.Errorf("problem while closing active client connection: %v", err)
	}
	return err
}

// Close shuts down the listening server and any active client connections.
func (conn *TCPServerConnection) Close() (closeErr error) {
	conn.estabMutex.Lock()
	if conn.IsClosed() {
		conn.estabMutex.Unlock()
		return nil // it's already been closed
	}

	conn.closed = true
	conn.closeInitiated = true
	conn.listener.SetDeadline(time.Now().Add(50 * time.Millisecond))
	select {
	case <-conn.doneSignal:
	case <-time.After(99 * time.Millisecond):
		conn.log.traceCb("clean close timed out after short timeout; forcing unclean close")
	}

	serverErr := conn.listener.Close()
	conn.estabMutex.Unlock()

	clientErr := conn.synchedInvalidateEstab()

	if serverErr != nil {
		closeErr = fmt.Errorf("problem closing server listener: %v", serverErr)
	}
	if clientErr != nil {
		if closeErr != nil {
			closeErr = fmt.Errorf("%v, additionally encountered problem while closing active client connection: %v", closeErr, clientErr)
		} else {
			closeErr = fmt.Errorf("problem while closing active client connection: %v", clientErr)
		}
	}
	return
}

// Send sends binary data over the connection. A response is not waited for, though depending on the
// connection a non-nil error indicates that a message was received (as is the case in TCP with an
// ACK in response to a client PSH.)
func (conn *TCPServerConnection) Send(data []byte) error {
	errNoClient := fmt.Errorf("this server connection doesn't currently have a client to communicate with")
	if !conn.Ready() {
		return errNoClient
	}
	if conn.IsClosed() {
		return fmt.Errorf("this connection has been closed and can no longer be used to send")
	}

	conn.estabMutex.Lock()
	defer conn.estabMutex.Unlock()
	if conn.estab == nil {
		return errNoClient
	}
	return conn.estab.Send(data)
}

// Ready returns whether this connection is ready to send bytes. Attempting to call Send()
// before Ready() returns true will result in an error.
//
// Note that a closed connection will return true as well.
func (conn *TCPServerConnection) Ready() bool {
	return conn.synchedClientIsConnected()
}

// GetRemoteName returns the host that was connected to
func (conn *TCPServerConnection) GetRemoteName() string {
	if !conn.Ready() {
		return ""
	}
	clientAddr := conn.synchedClientAddr()
	if clientAddr == nil {
		return ""
	}
	return clientAddr.String()
}

// GetLocalName returns the name of the local side of the connection.
func (conn *TCPServerConnection) GetLocalName() string {
	return conn.listener.Addr().String()
}

// GotTimeout returns whether the initial connection timed out.
func (conn *TCPServerConnection) GotTimeout() bool {
	return conn.timedOut
}

func (conn *TCPServerConnection) startListening() {
	go func() {
		defer close(conn.doneSignal)
		defer func() {
			if conn.estab != nil { // unsafe check first for speed, then safe check - TODO: probably a bad idea, check
				conn.estabMutex.Lock()
				defer conn.estabMutex.Unlock()
				if conn.estab != nil {
					if err := conn.estab.Close(); err != nil {
						conn.log.debugCb("got error when closing established connection: %v", err)
					}
					conn.estab = nil
					conn.estabClientAddr = nil
				}
			}
		}()
		for !conn.closeInitiated && !conn.closed {
			conn.log.traceCb("starting to check for connections...")

			// about to use "timeout deadline" several times, establish a single point now.
			timeoutDeadline := time.Now().Add(conn.timeout)
			// we do not allow any connections after the first so this should only come up once
			// in this for-loop, but have the checks in case we later decide to extend to accepting
			// multiple or more after the first.

			// if timeout requested
			if conn.timeout != 0 {
				conn.log.traceCb("applying timeout to listen...")
				if err := conn.listener.SetDeadline(timeoutDeadline); err != nil {
					conn.log.debugCb("problem setting listener deadline: %v", err)
				}
			}
			conn.log.traceCb("listening for client connection...")
			clientSock, err := conn.listener.AcceptTCP()
			conn.log.traceCb("stopped listening for client connection...")
			// if timeout is requested
			if conn.timeout != 0 {
				if err != nil {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						if conn.closeInitiated {
							// handle condition of listening for first connection but
							// close requested prior to then (via Ctrl-C)
							// don't print any messages, just continue.
							//
							// not doing timedOut = true bc this is not the caller-requested timeout,
							// but from an internal one set by Close().
							continue
						}
						if !conn.synchedClientIsConnected() {
							conn.timedOut = true
							conn.log.errorCb(err, "timed out while waiting for connection")
							conn.Close()
						}
						continue
					}
					// else it will be handled by next error check
				}
				if err := conn.listener.SetDeadline(time.Time{}); err != nil {
					conn.log.debugCb("problem unsetting listener deadline: %v", err)
				}
				// there is a race condition - Close() will call SetDeadline on the listener to attempt
				// to make it stop listening. If it had JUST prior to the above call set it, it will then
				// be removed. In this case, the Close() function is set up to force it closed after it detects
				// that this routine is not exiting.
				if conn.closeInitiated {
					continue
				}
			}

			if err != nil {
				conn.log.errorCb(err, "could not accept client connection: %v", err)
				go conn.Close()
				continue
			}

			if conn.synchedClientIsConnected() {
				// nope, this is an interactive console and we cant have more than one
				conn.log.traceCb("rejected connection from client at %v due to already being in active communication with another", clientSock.RemoteAddr().String())
				continue
			}

			tlsHandshakeDeadline := time.Time{}
			if conn.tlsConf != nil && conn.timeout != 0 {
				maxTLSHandshakeDeadline := time.Now().Add(10 * time.Second)
				if timeoutDeadline.After(maxTLSHandshakeDeadline) {
					tlsHandshakeDeadline = maxTLSHandshakeDeadline
				} else {
					tlsHandshakeDeadline = timeoutDeadline
				}
				tlsHandshakeDeadline = timeoutDeadline

				conn.log.debugCb("waiting until %s for TLS client hello...", tlsHandshakeDeadline.Format(time.RFC3339))
			}

			conn.synchedHandleAccept(clientSock, tlsHandshakeDeadline)
		}
	}()
}

func (conn *TCPServerConnection) synchedClientAddr() net.Addr {
	conn.estabMutex.Lock()
	defer conn.estabMutex.Unlock()
	return conn.estabClientAddr
}

func (conn *TCPServerConnection) synchedClientIsConnected() bool {
	conn.estabMutex.Lock()
	defer conn.estabMutex.Unlock()
	if conn.estab != nil {
		return true
	}
	return false
}

// this does not return an error so caller can continue accepting next connection and either taking or rejecting.
func (conn *TCPServerConnection) synchedHandleAccept(clientSock *net.TCPConn, tlsHandshakeDeadline time.Time) {
	conn.log.traceCb("accepting connection...")
	var err error
	conn.estabMutex.Lock()
	defer conn.estabMutex.Unlock()
	conn.estab, err = newTCPConnectionFromAccept(conn.onRecv, conn.log, conn.keepAlives, conn.tlsConf, tlsHandshakeDeadline, clientSock, conn.synchedInvalidateEstab)
	if err != nil {
		if errors.Is(err, os.ErrDeadlineExceeded) {
			conn.log.debugCb("abandoning connection; client did not send TLS hello within handshake timeout period")
		} else {
			conn.log.debugCb("abandoning connection; could not create TCP connection to client: %v", err)
		}
		return
	}
	conn.estabClientAddr = clientSock.RemoteAddr()
	// do it in a go routine so it breaking doesn't blow up the accept loop
	go conn.onConnect(clientSock.RemoteAddr().String())
}

func (conn *TCPServerConnection) synchedInvalidateEstab() error {
	conn.estabMutex.Lock()
	defer conn.estabMutex.Unlock()
	var err error
	if conn.estab != nil {
		if err = conn.estab.Close(); err != nil {
			conn.log.debugCb("problem closing established after invalidation: %v", err)
		}
		conn.estab = nil
	}
	return err
}
