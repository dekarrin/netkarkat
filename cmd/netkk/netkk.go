package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"dekarrin/netkarkat/internal/console"
	"dekarrin/netkarkat/internal/driver"
	"dekarrin/netkarkat/internal/verbosity"

	"gopkg.in/alecthomas/kingpin.v2"
)

const (
	currentVersion = "0.1.0"

	// ExitStatusIOError is the exit status given by a failure to read or write a file.
	ExitStatusIOError = 4

	// ExitStatusArgumentsError is the exit status given when there is a problem parsing the arguments.
	ExitStatusArgumentsError = 3

	// ExitStatusScriptCommandError is the exit status given when there is a problem with a command in a script or passed directly to netkk.
	ExitStatusScriptCommandError = 2

	// ExitStatusGenericError is the exit status given by an error not already covered by a more specific status code.
	ExitStatusGenericError = 1

	// ExitSuccess is the result for successful exit.
	ExitSuccess = 0
)

var returnCode int = ExitSuccess

func main() {
	defer func() {
		if panicErr := recover(); panicErr != nil {
			// we are panicking; don't let the check stop the panic
			panic("unrecoverable panic occured")
		} else {
			os.Exit(returnCode)
		}
	}()

	// declare options used in program
	var remoteHost string
	var remotePort int
	var localAddress string
	var localPort int

	// parse cli options
	protocolFlag := kingpin.Flag("protocol", "Which protocol to use.").Default("tcp").Short('p').Enum("tcp", "udp")
	remoteFlag := kingpin.Flag("remote", "The remote host to connect to; can be an IP address or hostname. Must be in HOST_ADDRESS:PORT form.").Short('r').String()
	listenFlag := kingpin.Flag("listen", "Give the local port to listen on/bind to. If none given, an ephemeral port is automatically chosen. Must be either in BIND_ADDRESS:PORT form or just be PORT form, in which case 127.0.0.1 is used as the bind address.").Short('l').String()
	timeoutFlag := kingpin.Flag("timeout", "How long to wait (in seconds) for the initial connection before timing out. Always valid for TCP, but only valid for UDP when in listen-mode.").Default("30").Short('t').Int()
	commandFlag := kingpin.Flag("command", "Byte(s) to send (or commands to execute), after which the program exits. Comes before script file execution if both set. If any send fails, this program will immediately terminate and return non-zero without executing the rest of the commands or scripts.").Short('C').Strings()
	scriptFileFlag := kingpin.Flag("script-file", "Script(s) to execute, after which the program exits. Script files are executed in order they appear. If any command fails, this program will immediately terminate and return non-zero without executing the rest of the commands or scripts.").Short('f').ExistingFiles()
	logFileFlag := kingpin.Flag("log", "Create a detailed system log file at the given location.").OpenFile(os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0766)
	multilineModeFlag := kingpin.Flag("multiline", "Do not send input when enter is pressed; continuing reading input until a semicolon is encountered.").Short('M').Bool()
	quietFlag := kingpin.Flag("quiet", "Silence all output except for server results. Overrides verbose mode.").Short('q').Bool()
	useTLSFlag := kingpin.Flag("tls", "Enable SSL/TLS for the connection.").Bool()
	macrofileFlag := kingpin.Flag("macrofile", "File to load for macros instead of the default one. Will also be where they are saved to.").Short('m').ExistingFile()
	skipVerifyFlag := kingpin.Flag("insecure-skip-verify", "Do not verify remote host server certificates when using SSL/TLS.").Bool()
	trustChainFileFlag := kingpin.Flag("trustchain", "File to use to verify remote host server certificates when using SSL/TLS.").ExistingFile()
	serverCertFileFlag := kingpin.Flag("server-cert", "PEM cert file to use for encrypting SSL/TLS connections as a TCP server.").ExistingFile()
	serverKeyFileFlag := kingpin.Flag("server-key", "PEM private key file to use for encrypting SSL/TLS connections as a TCP server.").ExistingFile()
	serverCertCnFlag := kingpin.Flag("cert-common-name", "The common name to use for a self-signed cert when using an SSL/TLS-enabled TCP server.").Default("localhost").String()
	serverCertIPsFlag := kingpin.Flag("cert-ips", "The IPs to list in a self-signed cert when using an SSL/TLS-enabled TCP server.").IPList()
	noPromptFlag := kingpin.Flag("no-prompt", "Disable the prompt text giving info on the connected remote host.").Bool()
	noKeepalivesFlag := kingpin.Flag("no-keepalives", "Disable keepalives in protocols that support them (TCP).").Bool()
	verboseFlag := kingpin.Flag("verbose", "Make output more verbose; up to 3 can be specified for increasingly verbose output.").Short('v').Counter()

	kingpin.Version(currentVersion)
	kingpin.CommandLine.HelpFlag.Short('h')
	kingpin.Parse()

	interactiveMode := true
	if len(*commandFlag) > 0 || len(*scriptFileFlag) > 0 {
		// we are going into command mode, do not do interactive console
		interactiveMode = false
	}

	outVerb := verbosity.ParseFromFlags(*quietFlag, *verboseFlag)
	out := verbosity.OutputWriter{Verbosity: outVerb, AutoNewline: true, AutoCapitalize: true}

	if *logFileFlag != nil {
		out.StartLogging(*logFileFlag)
	}

	if *listenFlag == "" && *remoteFlag == "" {
		handleFatalErrorWithStatusCode(fmt.Errorf("at least one of -l or -r must be specified"), ExitStatusArgumentsError)
		return
	}

	if *remoteFlag != "" {
		var err error
		remoteHost, remotePort, err = parseSocketAddressFlag(*remoteFlag)
		if err != nil {
			handleFatalErrorWithStatusCode(fmt.Errorf("remote address: %v", err), ExitStatusArgumentsError)
			return
		}
	}
	if *listenFlag != "" {
		var err error
		localAddress, localPort, err = parseListenAddressFlag(*listenFlag)
		if err != nil {
			handleFatalErrorWithStatusCode(fmt.Errorf("listen/local address: %v", err), ExitStatusArgumentsError)
			return
		}
	}

	connConf := driver.Options{
		TLSEnabled:              *useTLSFlag,
		TLSSkipVerify:           *skipVerifyFlag,
		TLSTrustChain:           *trustChainFileFlag,
		TLSServerCertFile:       *serverCertFileFlag,
		TLSServerKeyFile:        *serverKeyFileFlag,
		TLSServerCertCommonName: *serverCertCnFlag,
		TLSServerCertIPs:        *serverCertIPsFlag,
		ConnectionTimeout:       time.Duration(*timeoutFlag) * time.Second,
		DisableKeepalives:       *noKeepalivesFlag,
	}

	if err := validateSSLOptions(&connConf, *protocolFlag, localAddress, localPort, remoteHost, remotePort, out); err != nil {
		handleFatalErrorWithStatusCode(err, ExitStatusArgumentsError)
		return
	}

	var lastConnectionError error
	cbs := driver.NewLoggingCallbacks(out.Trace, out.Debug, out.Warn, func(err error, format string, a ...interface{}) {
		lastConnectionError = err

		// don't print eof
		if err != io.EOF {
			out.Error(format, a...)
		}
	})

	printRemoteMessage := func(data []byte) {
		prettyHexStr := ""
		for _, b := range data {
			prettyHexStr += fmt.Sprintf("0x%s ", hex.EncodeToString([]byte{b}))
		}
		if *noPromptFlag {
			out.Info("> %s\n", strings.TrimSpace(prettyHexStr))
		} else {
			out.Info("REMOTE>> %s\n", strings.TrimSpace(prettyHexStr))
		}
	}

	if (interactiveMode || out.Verbosity.Allows(verbosity.Debug)) && remoteHost != "" {
		out.Info("Connecting to %s:%d...\n", remoteHost, remotePort)
	}

	var conn driver.Connection
	var err error

	switch *protocolFlag {
	case "tcp":
		if remoteHost != "" {
			conn, err = driver.OpenTCPClient(printRemoteMessage, cbs, remoteHost, remotePort, localPort, connConf)
		} else {
			showConnected := func(host string) {
				fmt.Printf("Client connected from %v\n", host)
			}
			conn, err = driver.OpenTCPServer(printRemoteMessage, showConnected, cbs, localAddress, localPort, connConf)
		}
	case "udp":
		conn, err = driver.OpenUDPConnection(printRemoteMessage, cbs, remoteHost, remotePort, localAddress, localPort, connConf)
	default:
		handleFatalErrorWithStatusCode(fmt.Errorf("unknown protocol: %v", *protocolFlag), ExitStatusArgumentsError)
		return
	}
	if err != nil {
		handleFatalError(err)
		if remoteHost != "" {
			sslSupportRequiredText := "non-SSL"
			if connConf.TLSEnabled {
				sslSupportRequiredText = "SSL"
			}
			fmt.Fprintf(os.Stderr, "Ensure the remote server is up and supports %s %v connections\n", sslSupportRequiredText, strings.ToUpper(*protocolFlag))
		}
		return
	}

	var promptErr error
	defer func() {
		if interactiveMode && promptErr == nil {
			out.Info("Closing connection...\n")
		}
		closeErr := conn.Close()
		if closeErr != nil {
			out.Warn("%v", closeErr)
		}
	}()

	if interactiveMode || out.Verbosity.Allows(verbosity.Debug) {
		if remoteHost != "" {
			out.Info("Connection established; local side is %v\n", conn.GetLocalName())
		} else {
			out.Info("Listening for %v connections on %v...\n", strings.ToUpper(*protocolFlag), conn.GetLocalName())
		}
	}

	if interactiveMode {
		promptErr = console.StartPrompt(conn, out, currentVersion, *multilineModeFlag, !*noPromptFlag, *macrofileFlag)
		if promptErr != nil {
			if lastConnectionError == io.EOF {
				// it will not have been printed yet bc of our error handler given to the connection, we need to do that now
				// IF we are in verbose mode. else the term just exits and the user can assume that is what happened.

				// EOF is okay; don't print it unless in verbose, there are many cases the host could close connection
				// and there is nothing for us to do about it.
				out.Debug("%v: got EOF", promptErr)
			} else if !conn.GotTimeout() { // dont print additional message on connection timeout.
				handleFatalErrorWithStatusCode(promptErr, ExitStatusIOError)
				return
			}
		}
	} else {
		// we have scripts or commands to execute
		for idx, cmdArg := range *commandFlag {
			_, err := console.ExecuteScript(strings.NewReader(cmdArg), conn, out, currentVersion, *multilineModeFlag, *macrofileFlag)
			if err != nil {
				handleFatalErrorWithStatusCode(fmt.Errorf("command #%d: %v", idx+1, err), ExitStatusScriptCommandError)
				return
			}
		}
		for _, filename := range *scriptFileFlag {
			f, err := os.Open(filename)
			if err != nil {
				handleFatalErrorWithStatusCode(fmt.Errorf("problem opening %q: %v", filename, err), ExitStatusIOError)
			}
			defer f.Close()

			lines, err := console.ExecuteScript(f, conn, out, currentVersion, !*multilineModeFlag, *macrofileFlag)
			if err != nil {
				handleFatalErrorWithStatusCode(fmt.Errorf("%q:%d: %v", filename, lines+1, err), ExitStatusScriptCommandError)
				return
			}
			out.Debug("Executed %d lines in %q", lines, filename)
		}
	}
}

func validateSSLOptions(conf *driver.Options, protocol string, localAddress string, localPort int, remoteAddress string, remotePort int, out verbosity.OutputWriter) error {
	// find out if we're about to connect to another host or if we will wait
	// for someone to connect to us
	startAsServer := remoteAddress == ""

	if conf.TLSEnabled {
		if protocol == "udp" {
			return fmt.Errorf("--ssl given for UDP but SSL/TLS over UDP (DTLS) is not supported")
		} else if protocol == "tcp" {
			if startAsServer {
				if (conf.TLSServerCertFile == "" && conf.TLSServerKeyFile != "") || (conf.TLSServerCertFile != "" && conf.TLSServerKeyFile == "") {
					return fmt.Errorf("if one of --server-cert or --server-key are provided, they must both be given")
				}
				if conf.TLSSkipVerify {
					return fmt.Errorf("--insecure-skip-verify option cannot be set for a server connection")
				}
				if conf.TLSTrustChain != "" {
					return fmt.Errorf("--trustchain option specified for server but client auth is not yet implemented")
				}
				if conf.TLSServerCertFile == "" {
					out.Warn("--server-cert and --server-key not provided; netkk will use a self-signed CA to generate a cert")
				} else {
					if conf.TLSServerCertCommonName != "" {
						out.Warn("--server-cert and --server-key are provided so --cert-common-name is ignored")
						conf.TLSServerCertCommonName = ""
					}
					if len(conf.TLSServerCertIPs) > 0 {
						out.Warn("--server-cert and --server-key are provided so --cert-ips is ignored")
						conf.TLSServerCertIPs = nil
					}
				}
			} else {
				if conf.TLSServerKeyFile != "" {
					return fmt.Errorf("--server-key-file cannot be given for TCP client connections")
				}
				if conf.TLSServerCertFile != "" {
					return fmt.Errorf("--server-cert-file cannot be given for TCP client connections")
				}
				if conf.TLSServerCertCommonName != "" {
					return fmt.Errorf("--cert-common-name cannot be given for TCP client connections")
				}
				if len(conf.TLSServerCertIPs) > 0 {
					return fmt.Errorf("--cert-ips cannot be given for TCP client connections")
				}
				if conf.TLSSkipVerify {
					out.Warn("--insecure-skip-verify given; server certificate will be not be verified")
				}
			}
		}
	} else {
		if conf.TLSTrustChain != "" {
			out.Warn("--trustchain option given but SSL is not enabled; ignoring")
			conf.TLSTrustChain = ""
		}
		if conf.TLSSkipVerify {
			out.Warn("--insecure-skip-verify option set but SSL is not enabled; ignoring")
			conf.TLSSkipVerify = false
		}
	}
	return nil
}

// does proper handling of error and then exits
func handleFatalError(err error) {
	handleFatalErrorWithStatusCode(err, ExitStatusGenericError)
}

// does proper handling of error and then exits
func handleFatalErrorWithStatusCode(err error, retCode int) {
	// don't panic, ever. Just output the generic error message
	fmt.Fprintf(os.Stderr, "%v\n", err)
	returnCode = retCode
}

func parseSocketAddressFlag(unparsed string) (string, int, error) {
	parts := strings.SplitN(unparsed, ":", 2)
	if len(parts) < 2 {
		return "", 0, fmt.Errorf("must be in HOST:PORT form")
	}
	host := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a valid port number", parts[1])
	}
	if port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("%q is not a valid port; must be between 1 and 65535", parts[1])
	}

	return host, port, nil
}

func parseListenAddressFlag(unparsed string) (string, int, error) {
	addr := "127.0.0.1"

	// first try to parse as-is as an int
	port, err := strconv.Atoi(unparsed)
	if err != nil {
		addr, port, err = parseSocketAddressFlag(unparsed)
		if err != nil {
			if len(strings.SplitN(unparsed, ":", 2)) < 2 {
				return "", 0, fmt.Errorf("must be in HOST:PORT form or PORT form")
			}
			return "", 0, err
		}
	} else {
		// range check
		if port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("%q is not a valid port; must be between 1 and 65535", unparsed)
		}
	}
	return addr, port, nil
}
