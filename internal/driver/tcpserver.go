package driver

import (
	"crypto/tls"
	"crypto/x509"
	"dekarrin/netkarkat/internal/certs"
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
	listener    *net.TCPListener
	log         LoggingCallbacks
	doneSignal  chan struct{}
	accepting   bool
	firstClient net.Addr

	// estab is used by multiple go routines. all access must be synched via estabMutex.
	estab      *TCPConnection
	estabMutex sync.Mutex

	timeout    time.Duration
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
		accepting:  true,
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
			fmt.Printf("Wrote self-signed CA to %q", caFilename)

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

// IsClosed checks if the connection has been closed. This will be true after
// the first client has connected and then that connection has been closed.
func (conn *TCPServerConnection) IsClosed() bool {
	if !conn.accepting {
		conn.estabMutex.Lock()
		defer conn.estabMutex.Unlock()
		if conn.estab != nil {
			return conn.estab.IsClosed()
		}
		return true
	}
	return false
}

// Close shuts down the listening server and any active client connections.
func (conn *TCPServerConnection) Close() error {
	if conn.IsClosed() {
		return nil // it's already been closed
	}

	var err error
	conn.accepting = false
	conn.listener.SetDeadline(time.Now().Add(50 * time.Millisecond))
	select {
	case <-conn.doneSignal:
	case <-time.After(5 * time.Second):
		conn.log.warnCb("clean close timed out after 5 seconds; forcing unclean close")
	}

	var serverErr, clientErr error
	serverErr = conn.listener.Close()

	conn.estabMutex.Lock()
	if conn.estab != nil {
		clientErr = conn.estab.Close()
	}
	conn.estabMutex.Unlock()

	if serverErr != nil {
		err = fmt.Errorf("problem closing server listener: %v", err)
	}
	if clientErr != nil {
		if err != nil {
			err = fmt.Errorf("%v, additionally encountered problem while closing active client connection: %v", err, clientErr)
		} else {
			err = fmt.Errorf("problem while closing active client connection: %v", clientErr)
		}
	}

	return err
}

// Send sends binary data over the connection. A response is not waited for, though depending on the
// connection a non-nil error indicates that a message was received (as is the case in TCP with an
// ACK in response to a client PSH.)
func (conn *TCPServerConnection) Send(data []byte) error {
	if !conn.Ready() {
		return fmt.Errorf("this server connection doesn't yet have a client to communicate with")
	}
	if conn.IsClosed() {
		return fmt.Errorf("this connection has been closed and can no longer be used to send")
	}
	conn.estabMutex.Lock()
	defer conn.estabMutex.Unlock()
	return conn.estab.Send(data)
}

// Ready returns whether this connection is ready to send bytes. Attempting to call Send()
// before Ready() returns true will result in an error.
//
// Note that a closed connection will return true as well.
func (conn *TCPServerConnection) Ready() bool {
	return conn.firstClient != nil && !conn.accepting
}

// GetRemoteName returns the host that was connected to
func (conn *TCPServerConnection) GetRemoteName() string {
	if !conn.Ready() {
		return ""
	}
	return conn.firstClient.String()
}

// GetLocalName returns the name of the local side of the connection.
func (conn *TCPServerConnection) GetLocalName() string {
	return conn.listener.Addr().String()
}

func (conn *TCPServerConnection) startListening() {
	go func() {
		defer close(conn.doneSignal)
		defer func() { conn.accepting = false }()
		for conn.accepting {
			clientSock, err := conn.listener.AcceptTCP()
			if err != nil {
				conn.log.errorCb(err, "could not accept client connection: %v", err)
			}

			if !conn.accepting {
				conn.log.debugCb("rejected connection from client at %v due to already being in active communication with another", clientSock.RemoteAddr())
			}

			if conn.firstClient == nil {
				conn.firstClient = clientSock.RemoteAddr()
				conn.log.debugCb("first client has connected: %v", clientSock.RemoteAddr())
			} else if conn.firstClient != clientSock.RemoteAddr() {
				err := clientSock.Close()
				if err != nil {
					conn.log.warnCb("while closing client connection, got an error: %v", err)
				}
				conn.log.debugCb("rejected connection from non-first client at %v", clientSock.RemoteAddr())
			}

			conn.estabMutex.Lock()
			conn.estab, err = newTCPConnectionFromAccept(conn.onRecv, conn.log, conn.keepAlives, conn.tlsConf, clientSock)
			conn.estabMutex.Unlock()
			if err != nil {
				conn.log.warnCb("could not create TCP connection to client: %v", err)
			}
			conn.accepting = false

			// do it in a go routine so it breaking doesn't blow up the accept loop
			go conn.onConnect(clientSock.RemoteAddr().String())
		}
	}()
}
