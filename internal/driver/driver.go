package driver

import (
	"net"
	"time"
)

// maximum number of bytes that can be read from the network layer at once
const readerBufferSize = 1024

// ReceiveHandler is used on calls to open to register a function to call when bytes are received.
// The bytes are passed to the ReceiveHandler in a new goroutine, so there is no risk if there is
// a problem with the handler.
type ReceiveHandler func([]byte)

// ClientConnectedHandler is used as a hook for when a new client connects in protocols where
// the server end listens for new connections. The actual behavior and reading of the connection
// is done by the actual Connection unless otherwise stated; this function is called only to
// inform callers of when a new client connects.
type ClientConnectedHandler func(remoteAddress string)

// Options is options to a connection.
type Options struct {

	// Enables TLS on the connection. Currently only applicable for TCP.
	TLSEnabled bool

	// TLSSkipVerify disables all host verification. Not safe for production use. Ignored if
	// TLS is not enabled.
	TLSSkipVerify bool

	// TLSTrustChain is the path to the trust chain file for host verification. Ignored if
	// TLS is not enabled or if TLSSkipVerify is set to true.
	TLSTrustChain string

	// TLSServerCertFile is the path to the server certificate. Only used for listening TCP
	// connections; if TLS is specified but either this or TLSServerKeyFile are empty, a
	// new self-signed key will be generated instead of using the cert file.
	TLSServerCertFile string

	// TLSServerKeyFile is the path to the server certificate. Only used for listening TCP
	// connections; if TLS is specified but either this or TLSServerKeyFile are empty, a
	// new self-signed key will be generated instead of using the cert file.
	TLSServerKeyFile string

	// TLSServerCertCommonName is the common name used when generating a self-signed
	// certificate. Ignored if TLSServerCertFile and TLSServerKeyFile are set.
	TLSServerCertCommonName string

	// TLSServerCertIPs is the IP addresses used when generating a self-signed
	// certificate. Ignored if TLSServerCertFile and TLSServerKeyFile are set.
	TLSServerCertIPs []net.IP

	// ConnectionTimeout is how soon to give up on a connection. Zero value is no timeout.
	ConnectionTimeout time.Duration

	// DisableKeepalives specifies whether to turn off the typical keepalive messages for TCP.
	DisableKeepalives bool
}

// Connection is a connection to a remote host. It should generally be closed after use, though some
// protocols do not require this.
type Connection interface {

	// IsClosed checks if the connection has been closed.
	IsClosed() bool

	// Close shuts down the connection contained in the given object.
	// After the connection has been closed, it cannot be used to send any more messages.
	Close() error

	// Send sends binary data over the connection. A response is not waited for, though depending on the
	// connection a nil error indicates that a message was received (as is the case in TCP with an
	// ACK in response to a client PSH.)
	Send(data []byte) error

	// GetRemoteName gets the name of the remote host that was connected to.
	GetRemoteName() string

	// GetLocalName gets the name of the local side of the connection. This could be a port or something else specific to protocol.
	GetLocalName() string

	// Ready checks whether a connection is ready to have bytes sent on it. This may be false at startup for protocols
	// that listen for a connection between starting, such as TCP server.
	//
	// Note that this will return true even after the connection has been closed.
	Ready() bool

	// GotTimeout checks whether the initial connection/listen timed out, thus leading to the driver no longer being operable.
	// The driver must still be closed even if this returns true.
	GotTimeout() bool
}

// LogFormatter is a string format function that is used in
// LoggingCallbacks.
type LogFormatter func(string, ...interface{})

// LogErrorFormatter is a string format function that is used in
// LoggingCallbacks.
type LogErrorFormatter func(error, string, ...interface{})

// LoggingCallbacks is used to store callbacks that are called when debug,
// trace, error, or warn events occur. Any callback being set to its zero
// value means that this module will produce no output for that event.
//
// Create one with NewLoggingCallbacks().
type LoggingCallbacks struct {

	// Called for extremely low-level events, such as the exact bytes received/sent.
	traceCb LogFormatter

	// Called for low-level events, such as the sending and receiving of messages.
	debugCb LogFormatter

	// Called for events that may indicate a future problem.
	warnCb LogFormatter

	// Called for events that cause a Connection to no longer be valid.
	errorCb LogErrorFormatter
}

func (lc LoggingCallbacks) isValid() bool {
	return lc.traceCb != nil && lc.warnCb != nil && lc.debugCb != nil && lc.errorCb != nil
}

// NewLoggingCallbacks accepts a series of format functions for logging and returns them
// packaged together in a LoggingCallbacks object.
//
// Arguments that are set to nil are converted to no-op functions in the returned
// struct.
// TODO: probs should call this something else because it is the only way to get the
// socket errors (via LogErrorFormatter) since reads are performed asynchronously.
func NewLoggingCallbacks(traceCb LogFormatter, debugCb LogFormatter, warnCb LogFormatter, errorCb LogErrorFormatter) LoggingCallbacks {
	lc := LoggingCallbacks{traceCb: traceCb, debugCb: debugCb, warnCb: warnCb, errorCb: errorCb}

	emptyFunc := func(_ string, _ ...interface{}) {}
	if lc.traceCb == nil {
		lc.traceCb = emptyFunc
	}
	if lc.debugCb == nil {
		lc.debugCb = emptyFunc
	}
	if lc.warnCb == nil {
		lc.warnCb = emptyFunc
	}
	if lc.errorCb == nil {
		lc.errorCb = func(_ error, _ string, _ ...interface{}) {}
	}

	return lc
}

func resolveHost(value string) (net.IP, error) {
	if ip := net.ParseIP(value); ip != nil {
		return ip, nil
	}
	addr, err := net.ResolveIPAddr("ip", value)
	if err != nil {
		return nil, err
	}
	return addr.IP, nil
}
