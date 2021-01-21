package main

import (
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"dekarrin/netkarkat/internal/connection"
	"dekarrin/netkarkat/internal/console"
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
	var host net.IP
	var port int
	var useSsl bool
	var trustChainCertFile string

	// parse cli options
	commandFlag := kingpin.Flag("command", "command(s) to execute, after which the program exits. Comes before script file execution if both set. If any command fails, this program will immediately terminate and return non-zero without executing the rest of the commands or scripts.").Short('C').Strings()
	connectionTimeoutFlag := kingpin.Flag("connection-timeout", "how long to wait (in seconds) for the initial connection before timing out").Default("10").Int()
	hostFlag := kingpin.Flag("host", "the host to connect to; if any are set, overrides all in config (if any)").Short('H').Required().ResolvedIP()
	skipVerifyFlag := kingpin.Flag("insecure-skip-verify", "do not verify server certificates when using SSL").Bool()
	logFileFlag := kingpin.Flag("log", "create a detailed system log file at the given location").Short('l').OpenFile(os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0766)
	optionalSemicolonsFlag := kingpin.Flag("optional-semicolons", "send each line to server instead of waiting for semicolon").Bool()
	portFlag := kingpin.Flag("port", "the port to connect to").Short('p').Required().Int()
	quietFlag := kingpin.Flag("quiet", "silence all output except for server results. Overrides verbose mode").Short('q').Bool()
	scriptFileFlag := kingpin.Flag("script-file", "script(s) to execute, after which the program exits. Script files are executed in order they appear. If any command fails, this program will immediately terminate and return non-zero without executing the rest of the commands or scripts.").Short('f').ExistingFiles()
	useSslFlag := kingpin.Flag("ssl", "enable SSL for the connection").Bool()
	responseTimeoutFlag := kingpin.Flag("timeout", "amount of time to wait (in seconds) after sending a command before it is considered timed out").Default("30").Short('t').Int()
	trustChainFileFlag := kingpin.Flag("trustchain", "file to use to verify server certificates when using SSL").ExistingFile()
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

	host = *hostFlag
	if *portFlag < 1 || *portFlag > 65535 {
		handleFatalErrorWithStatusCode(fmt.Errorf("invalid port: %v", *portFlag), ExitStatusArgumentsError)
		return
	}
	useSsl = *useSslFlag
	port = *portFlag

	if !useSsl {
		if flagIsProvided("trustchain", "") {
			out.Warn("--trustchain option given but SSL is not enabled; ignoring")
			*trustChainFileFlag = ""
		}
		if flagIsProvided("insecure-skip-verify", "") {
			out.Warn("--insecure-skip-verify option set but SSL is not enabled; ignoring")
			*skipVerifyFlag = false
		}
	}
	if flagIsProvided("trustchain", "t") {
		trustChainCertFile = *trustChainFileFlag
	}

	if *skipVerifyFlag {
		out.Warn("--insecure-skip-verify given; server certificate will be not be verified")
	}

	connConf := connection.Options{
		TLSEnabled:        useSsl,
		TLSSkipVerify:     *skipVerifyFlag,
		TLSTrustChain:     trustChainCertFile,
		ConnectionTimeout: time.Duration(*connectionTimeoutFlag) * time.Second,
		ResponseTimeout:   time.Duration(*responseTimeoutFlag) * time.Second,
	}

	if interactiveMode || out.Verbosity.Allows(verbosity.Debug) {
		out.Info("Connecting to %s:%d...\n", host, port)
	}

	var lastConnectionError error
	cbs := connection.NewLoggingCallbacks(out.Trace, out.Debug, out.Warn, func(err error, format string, a ...interface{}) {
		lastConnectionError = err

		// don't print eof
		if err != io.EOF {
			out.Error(format, a...)
		}
	})

	conn, err := connection.OpenTCPConnection(func(data []byte) {
		hexChars := []rune(hex.EncodeToString(data))
		prettyHexStr := ""
		for i := 0; i < len(hexChars); i += 2 {
			prettyHexStr += fmt.Sprintf("0x%v%v ", hexChars[i], hexChars[i+1])
		}
		fmt.Printf("HOST>> %s\n", strings.TrimSpace(prettyHexStr))
	}, cbs, host, port, connConf)
	if err != nil {
		handleFatalError(err)
		sslSupportRequiredText := "non-SSL"
		if useSsl {
			sslSupportRequiredText = "SSL"
		}
		fmt.Fprintf(os.Stderr, "Ensure the remote server is up and supports %s TCP connections\n", sslSupportRequiredText)
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
		out.Info("Connection established\n")
	}

	if interactiveMode {
		promptErr = console.StartPrompt(conn, out, currentVersion, "tcp", !*optionalSemicolonsFlag)
		if promptErr != nil {
			if lastConnectionError == io.EOF {
				// it will not have been printed yet bc of our error handler given to the connection, we need to do that now
				promptErr = fmt.Errorf("%v: got unexpected EOF", promptErr)
			}
			handleFatalErrorWithStatusCode(promptErr, ExitStatusIOError)
			return
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
