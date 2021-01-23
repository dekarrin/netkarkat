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
	commandFlag := kingpin.Flag("command", "command(s) to execute, after which the program exits. Comes before script file execution if both set. If any command fails, this program will immediately terminate and return non-zero without executing the rest of the commands or scripts.").Short('C').Strings()
	timeoutFlag := kingpin.Flag("timeout", "how long to wait (in seconds) for the initial connection before timing out. Only valid for TCP.").Default("10").Short('t').Int()
	remoteFlag := kingpin.Flag("remote", "the host to connect to. Must be in host_address:port form.").Short('r').String()
	skipVerifyFlag := kingpin.Flag("insecure-skip-verify", "do not verify server certificates when using SSL").Bool()
	logFileFlag := kingpin.Flag("log", "create a detailed system log file at the given location").OpenFile(os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0766)
	listenFlag := kingpin.Flag("listen", "give the local port to 'bind' to. If none given, an ephemeral port is automatically chosen. Must be either in bind_ip:port form or just be the port, in which case 0.0.0.0 is used as the bind address.").Short('l').String()
	optionalSemicolonsFlag := kingpin.Flag("optional-semicolons", "send each line to server instead of waiting for semicolon").Bool()
	quietFlag := kingpin.Flag("quiet", "silence all output except for server results. Overrides verbose mode").Short('q').Bool()
	scriptFileFlag := kingpin.Flag("script-file", "script(s) to execute, after which the program exits. Script files are executed in order they appear. If any command fails, this program will immediately terminate and return non-zero without executing the rest of the commands or scripts.").Short('f').ExistingFiles()
	useSslFlag := kingpin.Flag("ssl", "enable SSL for the connection").Bool()
	trustChainFileFlag := kingpin.Flag("trustchain", "file to use to verify server certificates when using SSL").ExistingFile()
	serverCertFileFlag := kingpin.Flag("server-cert", "PEM cert file to use for encrypting SSL connections as a TCP server").ExistingFile()
	serverKeyFileFlag := kingpin.Flag("server-key", "PEM private key file to use for encrypting SSL connections as a TCP server").ExistingFile()
	serverCertCnFlag := kingpin.Flag("cert-common-name", "Gives the common name to use for a self-signed cert when using an SSL-enabled TCP server").String()
	serverCertIPsFlag := kingpin.Flag("cert-ips", "Gives the IPs to list in a self-signed cert when using an SSL-enabled TCP server").IPList()
	protocolFlag := kingpin.Flag("protocol", "which protocol to use").Short('p').Enum("tcp", "udp")
	noKeepalivesFlag := kingpin.Flag("no-keepalives", "disables keepalives in protocols that support them").Bool()
	verboseFlag := kingpin.Flag("verbose", "make output more verbose; up to 3 can be specified for increasingly verbose output").Short('v').Counter()

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
			handleFatalErrorWithStatusCode(err, ExitStatusArgumentsError)
			return
		}
	}
	if *listenFlag != "" {
		var err error
		localAddress, localPort, err = parseListenAddressFlag(*remoteFlag)
		if err != nil {
			handleFatalErrorWithStatusCode(err, ExitStatusArgumentsError)
			return
		}
	}

	connConf := driver.Options{
		TLSEnabled:              *useSslFlag,
		TLSSkipVerify:           *skipVerifyFlag,
		TLSTrustChain:           *trustChainFileFlag,
		TLSServerCertFile:       *serverCertFileFlag,
		TLSServerKeyFile:        *serverKeyFileFlag,
		TLSServerCertCommonName: *serverCertCnFlag,
		TLSServerCertIPs:        *serverCertIPsFlag,
		ConnectionTimeout:       time.Duration(*timeoutFlag) * time.Second,
		DisableKeepalives:       *noKeepalivesFlag,
	}

	validateSSLOptions(&connConf, *protocolFlag, localAddress, localPort, remoteHost, remotePort, out)

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
		fmt.Printf("HOST>> %s\n", strings.TrimSpace(prettyHexStr))
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
		sslSupportRequiredText := "non-SSL"
		if connConf.TLSEnabled {
			sslSupportRequiredText = "SSL"
		}
		fmt.Fprintf(os.Stderr, "Ensure the remote server is up and supports %s %v connections\n", sslSupportRequiredText, strings.ToUpper(*protocolFlag))
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
		promptErr = console.StartPrompt(conn, out, currentVersion, *protocolFlag, !*optionalSemicolonsFlag)
		if promptErr != nil {
			if lastConnectionError == io.EOF {
				// it will not have been printed yet bc of our error handler given to the connection, we need to do that now
				// IF we are in verbose mode. else the term just exits and the user can assume that is what happened.

				// EOF is okay; don't print it unless in verbose, there are many cases the host could close connection
				// and there is nothing for us to do about it.
				out.Debug("%v: got EOF", promptErr)
			} else {
				handleFatalErrorWithStatusCode(promptErr, ExitStatusIOError)
				return
			}
		}
	} else {
		// we have scripts or commands to execute
		for idx, cmdArg := range *commandFlag {
			_, err := console.ExecuteScript(strings.NewReader(cmdArg), conn, out, currentVersion, !*optionalSemicolonsFlag)
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

			lines, err := console.ExecuteScript(f, conn, out, currentVersion, !*optionalSemicolonsFlag)
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

func flagIsProvided(longName string, shortNames string) bool {
	for _, arg := range os.Args {
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "--") {
			arg = strings.Split(arg, "=")[0]
			if arg == "--"+longName {
				return true
			}
		} else if !strings.HasPrefix(arg, "--") && strings.HasPrefix(arg, "-") {
			for _, shortName := range shortNames {
				for _, argChar := range arg {
					if shortName == argChar {
						return true
					}
				}
			}
		}
	}
	return false
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
	}
	return addr, port, nil
}
