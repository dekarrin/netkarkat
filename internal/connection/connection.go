package connection

import "time"

// ReceiveHandler is used on calls to open to register a function to call when bytes are received.
// The bytes are passed to the ReceiveHandler in a new goroutine, so there is no risk if there is
// a problem with the handler.
type ReceiveHandler func([]byte)

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

	// ConnectionTimeout is how soon to give up on a connection. Zero value is no timeout.
	ConnectionTimeout time.Duration

	// MessageTimeout is how soon to give up on receiving a response to a message. This is
	// only supported for protocols that have a clearly-defined "reply" mechanism such as TCP.
	ResponseTimeout time.Duration

	// DisableKeepalives specifies whether to turn off the typical keepalive messages for TCP.
	DisableKeepalives bool
}

// Connection is a connection to a remote host. It should generally be closed after use, though some
// protocols do not require this.
type Connection interface {

	// IsClosed checks if the connection has been closed.
	IsClosed() bool

	// Close shuts down the connection contained in the given object. Waits maximum 5 seconds after sending close before assuming that the close was successful.
	// After the connection has been closed, it cannot be used to send any more messages.
	Close() error

	// Send sends binary data over the connection. A response is not waited for, though depending on the
	// connection a nil error indicates that a message was received (as is the case in TCP with an
	// ACK in response to a client PSH.)
	Send(data []byte) error

	// Gets the name of the remote host that was connected to.
	GetRemoteName() string
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
