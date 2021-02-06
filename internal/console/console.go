package console

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"dekarrin/netkarkat/internal/driver"
	"dekarrin/netkarkat/internal/macros"
	"dekarrin/netkarkat/internal/persist"
	"dekarrin/netkarkat/internal/verbosity"

	"github.com/peterh/liner"
)

type errCloseDuringPrompt struct {
	afterPrefix bool
	invalid     bool
}

func (cdp errCloseDuringPrompt) Error() string {
	return "driver was closed"
}

const (
	panicCodeCloseWhilePromptOpenBeforePrefixPrint = 1
	panicCodeCloseWhilePromptOpenAfterPrefixPrint  = 2
)

func init() {
	initCommands()
}

type consoleState struct {
	connection           driver.Connection
	running              bool          // only valid if in interactive mode
	prompt               *liner.State  // only valid if in interactive mode
	userStore            persist.Store // only valid if in interactive mode
	version              string
	out                  verbosity.OutputWriter
	interactive          bool
	delimitWithSemicolon bool
	macrofile            string
	macros               macros.MacroCollection
}

func promptWithConnectionMonitor(state *consoleState, prefix string) (string, error) {
	type result struct {
		s string
		e error
	}

	inputCh := make(chan result, 1)

	// todo: think this might leak. this is technically okay for now
	// as the main program immediately exits but this could be a problem
	// in any other context
	go func() {
		defer func() {
			state.out.Trace("keyboard input reader exited")
		}()
		str, err := state.prompt.Prompt(prefix)
		inputCh <- result{s: str, e: err}
	}()

	// watch connection to make sure it's up, if it dies, immediately panic
	var promptResult result
	promptReturned := false
	for !promptReturned {
		select {
		case promptResult = <-inputCh:
			promptReturned = true
		case <-time.After(10 * time.Millisecond):
			// check in with the connection to make sure it hasn't gone to invalid state
			if state.connection.IsClosed() {
				if err := state.prompt.Close(); err != nil {
					state.out.Trace("on post-prompt close: %v", err)
				}
				return "", errCloseDuringPrompt{
					afterPrefix: true,
					invalid:     true,
				}
			}
			if !state.connection.Ready() {
				if err := state.prompt.Close(); err != nil {
					state.out.Trace("on post-prompt close: %v", err)
				}
				return "", errCloseDuringPrompt{
					afterPrefix: true,
					invalid:     false,
				}
			}
		}
	}
	return promptResult.s, promptResult.e
}

func stringifyResults(results interface{}) string {
	if results == nil {
		return ""
	}
	if dataList, ok := results.([]interface{}); ok {
		var sb strings.Builder
		for idx, item := range dataList {
			sb.WriteString(fmt.Sprintf("%v", item))
			if idx+1 < len(dataList) {
				sb.WriteRune('\n')
			}
		}
		return sb.String()
	}
	return fmt.Sprintf("%v", results)
}

func normalizeLine(line string) (result string) {
	cmd := strings.SplitN(line, "#", 2)[0]
	cmd = strings.SplitN(cmd, "//", 2)[0]
	cmd = strings.TrimFunc(cmd, unicode.IsSpace)
	return cmd
}

func isTerminatedStatement(state *consoleState, line string, terminator string) bool {
	cmd := normalizeLine(line)
	return strings.HasSuffix(cmd, terminator)
}

func (state consoleState) parseLineToBytes(line string) (data []byte, err error) {
	// first, preprocess by doing macro replacement
	line, err = state.macros.Apply(line)
	if err != nil {
		return nil, err
	}

	runes := []rune(line)

	buf := make([]byte, 128) // 128 bytes should be plenty for every character in existence. utf8 says max is 4 but have a bigger buffer bc we can and it may handle weird cases

	// manual iteration instead of for-range so we control
	// which char we are on
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if unicode.IsSpace(ch) {
			continue
		}
		if ch == '\\' {
			if i+1 >= len(runes) {
				return nil, fmt.Errorf("unterminated backslash at char index %d", i)
			}
			if runes[i+1] == '\\' {
				count := utf8.EncodeRune(buf, runes[i+1])
				data = append(data, buf[:count]...)
				i++
				continue
			} else if runes[i+1] == 'x' {
				// byte sequence
				if i+3 >= len(runes) {
					return nil, fmt.Errorf("unterminated byte sequence at char index %d", i)
				}
				hexStr := string(runes[i+2 : i+4])
				b, err := hex.DecodeString(hexStr)
				if err != nil {
					return nil, fmt.Errorf("malformed byte sequence at char index %d: %v", i, err)
				}
				data = append(data, b[0])
				i += 3
				continue
			} else {
				return nil, fmt.Errorf("unknown escaped character: %v", runes[i+1])
			}
		} else {
			count := utf8.EncodeRune(buf, ch)
			data = append(data, buf[:count]...)
		}
	}

	return data, nil
}

// isLocalCommand indicates whether the line was processed as a command to the shell as opposed to sent to the remote end.
func executeLine(state *consoleState, line string) (cmdOutput string, err error) {
	// setting a var and checking it on function exit to avoid modifying the state of potential panics.
	// TODO: this may be an antipattern; check if it is, and if so, just use a normal panic recovery/propagation.
	exitExpected := false
	defer func() {
		if !exitExpected && state != nil {
			state.out.Critical("Execution resulted in a fatal error; exiting and then dumping stack...")
		}
	}()
	normalLine := normalizeLine(line)
	if state.delimitWithSemicolon {
		normalLine = strings.TrimSuffix(normalLine, ";")
	}

	if normalLine == "" {
		state.out.Trace("not sending empty escaped input\n")
		exitExpected = true
		return "", nil
	}

	output, executed, err := commands.executeIfIsCommand(state, normalLine)
	if executed {
		exitExpected = true
		return output, err
	}

	// otherwise, assume it is a send
	_, err = executeCommandSend(state, "SEND "+normalLine, "SEND")
	if err != nil {
		exitExpected = true
		return "", err
	}
	exitExpected = true
	return "", nil
}

// ExecuteScript executes script input from the given reader.
// It ignores comments and considers a semicolon to denote the end of a statement.
//
// Returns the number of lines processed successfully.
//
// If an error is encountered, the number of lines that were executed is returned along
// with the error that was encountered.
//
// Each statement's result is output as an INFO-level message in the given OutputWriter.
//
// For each statement, the following is done:
//
// If it is a command to netkk, that command is exectued and the output is returned. Otherwise, it is forwarded
// to the connected server and that output is returned.
//
// Everything after a "#" or a "//" is ignored.
// If the provided line is empty after removing comments and trimming, no action is taken and the empty string
// is returned.
func ExecuteScript(f io.Reader, conn driver.Connection, out verbosity.OutputWriter, version string, delimitWithSemicolon bool, macrofile string) (lines int, err error) {
	state := &consoleState{connection: conn, version: version, out: out, interactive: false, delimitWithSemicolon: delimitWithSemicolon, macrofile: macrofile}
	state.loadMacrosFile()
	scanner := bufio.NewScanner(f)
	lineNum := 0
	numLinesRead := 0
	moreInputRequired := true
	cmd := ""

	for scanner.Scan() {
		lineNum++
		partialCmd := scanner.Text()
		normalPartial := normalizeLine(partialCmd)
		if normalPartial == "" {
			continue
		}
		cmd += normalPartial
		moreInputRequired = state.delimitWithSemicolon && !isTerminatedStatement(state, cmd, ";")
		if moreInputRequired {
			cmd += "\n"
		} else {
			cmdOutput, err := executeLine(state, cmd)
			if err != nil {
				return numLinesRead, err
			}
			showScriptLineOutput(out, cmdOutput)
			numLinesRead = lineNum
			cmd = ""
		}
	}
	if err = scanner.Err(); err != nil {
		return numLinesRead, err
	}

	// need to execute last command in case it did not end with a semi:
	if cmd != "" {
		cmdOutput, err := executeLine(state, cmd)
		if err != nil {
			return numLinesRead, err
		}
		showScriptLineOutput(out, cmdOutput)
		numLinesRead = lineNum
		cmd = ""
	}
	return numLinesRead, nil
}

// StartPrompt makes a prompt and starts it
func StartPrompt(conn driver.Connection, out verbosity.OutputWriter, version string, delimitWithSemicolon bool, showPromptText bool, macrofile string) (err error) {

	state := consoleState{
		running:              true,
		connection:           conn,
		out:                  out,
		version:              version,
		interactive:          true,
		delimitWithSemicolon: delimitWithSemicolon,
		macrofile:            macrofile,
	}

	// sleep until ready
	for !state.connection.Ready() {
		time.Sleep(101 * time.Millisecond)
		if state.connection.IsClosed() {
			return fmt.Errorf("driver was closed before it became ready")
		}
	}

	state.setupConsoleLiner()
	defer state.prompt.Close()

	printSplashTextArt(6, state.out)
	state.out.Info("[netkarkat v%v]\n", state.version)
	state.out.Info("HELP for help.\n")

	var prefix string
	for state.running {
		// if the connection has gone non-ready, stop running
		for !state.connection.Ready() {
			time.Sleep(101 * time.Millisecond)
			if state.connection.IsClosed() {
				state.running = false
				return fmt.Errorf("driver was closed before it became ready")
			}
		}

		if showPromptText {
			prefix = fmt.Sprintf("netkk@%s> ", conn.GetRemoteName())
		}

		// histCmd is same as cmd but with spaces instead of newlines for multiline input.
		// this is because peterh/liner cannot currently track the cursor position
		// if multiline strings are put into its history.
		cmd, histCmd, err := promptUntilFullStatement(&state, prefix)
		if isErrCloseDuringPrompt(err) {
			errClose := err.(errCloseDuringPrompt)
			if errClose.afterPrefix {
				// can only get here once the prompt has printed; print a new line so further messages are not after prompt
				fmt.Printf("\n")
			}
			if errClose.invalid {
				return errClose
			}
			fmt.Printf("Client disconnected\n")
			state.setupConsoleLiner()
			continue
		} else if err == liner.ErrPromptAborted {
			state.out.Debug("console was aborted\n")
			state.running = false
			continue
		} else if err == io.EOF {
			state.out.Debug("abandoning active connection due to ^D input\n")
			state.connection.CloseActive()
			continue
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "fatal error: %v\n", err)
		}

		if strings.TrimFunc(cmd, unicode.IsSpace) == "" {
			state.out.Trace("ignoring empty input\n")
			continue
		}

		state.prompt.AppendHistory(histCmd)
		state.writeHistFile()

		cmdOutput, err := executeLine(&state, cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			if state.connection.IsClosed() {
				state.running = false
			}
		} else if cmdOutput != "" {
			fmt.Printf("%s\n", cmdOutput)
		}
	}
	return nil
}

func (state *consoleState) setupConsoleLiner() {
	state.prompt = liner.NewLiner()
	state.prompt.SetCtrlCAborts(true)
	state.prompt.SetBeep(false)
	state.prompt.SetMultiLineMode(true)

	state.prompt.SetCompleter(func(line string) []string {
		return autoComplete(state, line)
	})
	state.loadPersistenceFiles()
}

func promptUntilFullStatement(state *consoleState, prefix string) (inputWithNewlines string, inputWithSpaces string, err error) {
	var histBuf *bytes.Buffer
	defer func() {
		if histBuf != nil {
			err := resumeHistory(state.prompt, histBuf)
			if err != nil {
				state.out.Warn("while resuming history, got an error: %v", err)
			}
		}
	}()

	moreInputRequired := true
	onFirstLineOfInput := true
	cmd := ""
	cmdWithSpaces := "" // because history goes crazy if fed a multi-line string

	firstLevelPrefix := prefix
	for moreInputRequired {
		if state.connection.IsClosed() {
			if err := state.prompt.Close(); err != nil {
				state.out.Trace("on pre-prompt close: %v", err)
			}
			return "", "", errCloseDuringPrompt{
				afterPrefix: false,
				invalid:     true,
			}
		}
		if !state.connection.Ready() {
			if err := state.prompt.Close(); err != nil {
				state.out.Trace("on pre-prompt close: %v", err)
			}
			return "", "", errCloseDuringPrompt{
				afterPrefix: false,
				invalid:     false,
			}
		}
		partialCmd, err := promptWithConnectionMonitor(state, prefix)
		if isErrCloseDuringPrompt(err) {
			return "", "", err
		} else if err == liner.ErrPromptAborted && !onFirstLineOfInput {
			// abort the multi-line, but not the entire program
			cmd = ""
			cmdWithSpaces = ""
			onFirstLineOfInput = true
			prefix = firstLevelPrefix
			continue
		} else if err != nil {
			return "", "", err
		}
		normalPartial := normalizeLine(partialCmd)
		if normalPartial == "" {
			continue
		}
		cmd += normalPartial
		cmdWithSpaces += normalPartial

		moreInputRequired = state.delimitWithSemicolon && !isTerminatedStatement(state, cmd, ";")
		if moreInputRequired {
			cmd += "\n"
			cmdWithSpaces += " "
			if onFirstLineOfInput {
				prefix = "> "

				var err error
				histBuf, err = suspendHistory(state.prompt)
				if err != nil {
					state.out.Warn("while suspending history, got an error: %v", err)
				}
			}
		}
		onFirstLineOfInput = false
	}
	return cmd, cmdWithSpaces, nil
}

func resumeHistory(prompt *liner.State, buf *bytes.Buffer) error {
	_, err := prompt.ReadHistory(buf)
	return err
}

func suspendHistory(prompt *liner.State) (*bytes.Buffer, error) {
	buf := bytes.NewBuffer([]byte{})
	_, err := prompt.WriteHistory(buf)
	if err != nil {
		return nil, err
	}
	prompt.ClearHistory()
	return buf, nil
}
func showScriptLineOutput(out verbosity.OutputWriter, cmdOutput string) {
	if cmdOutput != "" {
		out.Info("%s", cmdOutput)
	}
}

func isErrCloseDuringPrompt(err error) bool {
	if _, ok := err.(errCloseDuringPrompt); ok {
		return ok
	}
	return false
}
