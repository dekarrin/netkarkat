package console

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"dekarrin/netkarkat/internal/driver"
	"dekarrin/netkarkat/internal/macros"
	"dekarrin/netkarkat/internal/misc"
	"dekarrin/netkarkat/internal/verbosity"

	"github.com/google/shlex"
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

func buildHelpCommandName(name string) string {
	c := commands[name]
	allNames := strings.Join(commands.getAllAliasesOf(name), "/")
	colName := allNames + " "
	if c.helpInvoke != "" {
		colName += c.helpInvoke + " "
	}
	colName += " "
	return colName
}

func writeHelpForCommand(name string, sb *strings.Builder, descWidth int, leftColumnWidth int, nameSuffix string) {
	helpDescLines := misc.WrapText(commands[name].helpDesc, descWidth)
	helpDescLines = misc.JustifyTextBlock(helpDescLines, descWidth)

	cmdName := buildHelpCommandName(name)
	sb.WriteString(cmdName)
	for i := 0; i < leftColumnWidth-(utf8.RuneCountInString(cmdName)+utf8.RuneCountInString(nameSuffix)); i++ {
		sb.WriteRune(' ')
	}
	sb.WriteString(nameSuffix)
	sb.WriteString(helpDescLines[0])
	sb.WriteRune('\n')
	for i := 1; i < len(helpDescLines); i++ {
		for j := 0; j < leftColumnWidth; j++ {
			sb.WriteRune(' ')
		}
		sb.WriteString(helpDescLines[i])
		sb.WriteRune('\n')
	}
	sb.WriteRune('\n')
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

func showHelp() string {
	helpWidth := 80
	nameSuffix := "- "

	// first build the initial
	leftColumnWidth := -1
	for name, c := range commands {
		if c.aliasFor != "" {
			continue
		}
		colName := buildHelpCommandName(name)
		if utf8.RuneCountInString(colName) > leftColumnWidth {
			leftColumnWidth = utf8.RuneCountInString(colName)
		}
	}
	leftColumnWidth += utf8.RuneCountInString(nameSuffix)
	descWidth := helpWidth - leftColumnWidth
	for descWidth < 2 {
		helpWidth++
		descWidth = helpWidth - leftColumnWidth
	}
	sb := strings.Builder{}
	sb.WriteString("Commands:\n")
	for _, name := range commands.names() {
		if commands[name].aliasFor != "" {
			continue
		}
		if name == "HELP" || name == "EXIT" { // special cases; these come at the end
			continue
		}
		writeHelpForCommand(name, &sb, descWidth, leftColumnWidth, nameSuffix)
	}

	writeHelpForCommand("HELP", &sb, descWidth, leftColumnWidth, nameSuffix)
	writeHelpForCommand("EXIT", &sb, descWidth, leftColumnWidth, nameSuffix)

	suffix := `By default, input will be read until it ends with a semicolon. To change this behavior,
		use the '--optional-semicolons' flag at launch.

		Any input that does not match one of the built-in commands is sent to the
		remote server and the results are displayed.

		If ":>" is put at the beginning of input, everything after it will be sent to
		the remote server regardless of whether it matches a built-in command. If a
		":>" needs to be sent as the start of input to the remote server, as in
		":>input to server", simply put a ":>" in front of it, like so:
		":>:>input to server".`

	suffixLines := misc.WrapText(suffix, helpWidth)
	suffixLines = misc.JustifyTextBlock(suffixLines, helpWidth)
	for _, line := range suffixLines {
		sb.WriteString(line)
		sb.WriteRune('\n')
	}

	return sb.String()
}

type consoleState struct {
	language             string
	connection           driver.Connection
	running              bool         // only valid if in interactive mode
	prompt               *liner.State // only valid if in interactive mode
	usingHistFile        bool         // only valid if in interactive mode
	version              string
	out                  verbosity.OutputWriter
	interactive          bool
	delimitWithSemicolon bool
	macros               macros.MacroCollection
}

type command struct {
	interactiveOnly bool

	// can only have one of argsExec or lineExec; if argsExec is set, lineExec will be ignored.
	// In argsExec, index 0 of argv is always the command in uppercase.
	argsExec func(state *consoleState, argv []string) (string, error)
	// in lineExec, cmdName is always the command in uppercase.
	lineExec func(state *consoleState, line string, cmdName string) (string, error)
	helpDesc string

	// string shown after this name of the command in help; can be used to give variables.
	helpInvoke string

	// setting this to non-zero will make execs and helpDesc ignored; they will be taken from the command
	// given here. Caveat: string given here must exist as a key in the 'commands' map.
	aliasFor string
}

func (c command) exec(state *consoleState, argv []string, line string) (out string, err error) {
	if c.argsExec != nil {
		out, err = c.argsExec(state, argv)
	} else if c.lineExec != nil {
		out, err = c.lineExec(state, line, argv[0])
	} else {
		panic("command does not give either argsExec or lineExec")
	}
	return out, err
}

type commandList map[string]command

func (cl commandList) parseCommand(in string) (isCommand bool, cmdToExec command, argv []string) {
	cmdTokens, err := shlex.Split(in)
	if err != nil {
		return false, cmdToExec, nil
	}
	if len(cmdTokens) < 1 {
		return false, cmdToExec, nil
	}

	firstToken := strings.ToUpper(cmdTokens[0])
	cmd, ok := cl[firstToken]
	if !ok {
		return false, cmdToExec, nil
	}
	cmdTokens[0] = firstToken
	return true, cmd, cmdTokens
}

func (cl commandList) executeIfIsCommand(state *consoleState, in string) (out string, isCommand bool, err error) {
	parsed, cmd, argv := cl.parseCommand(in)
	if !parsed {
		return "", false, nil
	}

	if cmd.interactiveOnly && !state.interactive {
		aliasStr := strings.Join(cl.getAllAliasesOf(argv[0]), "/")
		return "", true, fmt.Errorf("%s command only available in interactive mode", aliasStr)
	}

	if cmd.aliasFor != "" {
		actualCmd, ok := commands[cmd.aliasFor]
		if !ok {
			panic("command is alias for " + cmd.aliasFor + " but that command doesn't exist")
		}
		cmd = actualCmd
	}

	// make sure first item in token list is normalized before passing to execution
	out, err = cmd.exec(state, argv, in)
	return out, true, err
}

func (cl commandList) getAllAliasesOf(cmdName string) []string {
	givenCmd, ok := cl[cmdName]
	if !ok {
		return []string{}
	}

	aliasTarget := cmdName
	if givenCmd.aliasFor != "" {
		aliasTarget = givenCmd.aliasFor
	}
	aliases := []string{}

	for cmdName, cmd := range cl {
		if cmd.aliasFor == aliasTarget {
			aliases = append(aliases, cmdName)
		}
	}

	sort.Strings(aliases)
	aliases = append([]string{aliasTarget}, aliases...)
	return aliases
}

func (cl commandList) names() []string {
	keys := make([]string, len(cl))
	idx := 0
	for k := range cl {
		keys[idx] = k
		idx++
	}
	sort.Strings(keys)
	return keys
}

var commands = commandList{
	"CLEARHIST": command{
		interactiveOnly: true,
		helpDesc:        "Clear the command history.",
		argsExec:        executeCommandClearhist,
	},
	"EXIT": command{
		interactiveOnly: true,
		helpDesc:        "Exit the interactive session",
		argsExec: func(state *consoleState, args []string) (string, error) {
			state.running = false
			return "", nil
		},
	},
	"QUIT": command{
		aliasFor: "EXIT",
	},
	"BYE": command{
		aliasFor: "EXIT",
	},
	"SEND": command{
		helpInvoke: "<bytes...>",
		helpDesc:   "Sends bytes. This command is assumed when no other command is given.",
		lineExec:   executeCommandSend,
	},
	"DEFINE": command{
		helpInvoke: "<macro> <bytes...>",
		helpDesc:   "Create a macro that can be typed instead of a sequence of bytes; after DEFINE is used, the supplied name will be interpreted to be the supplied bytes in any context that takes bytes. Macros can also be used in other macro definitions, and will update the macro they are in when their own contents change. Macro names are case-insensitive.",
		lineExec:   executeCommandDefine,
	},
	"UNDEFINE": command{
		helpInvoke: "[-r] <macro>",
		helpDesc:   "Remove the definition of an existing macro created in a previous call to DEFINE. By default, any other macros that included the removed macro in their definitions will simply keep them as the bytes that represent the characters in the deleted macro's name; to have them replace it with its previous contents and continue to function as before, give the -r flag. Macro names are case-insensitive.",
		argsExec:   executeCommandUndefine,
	},
	"LIST": command{
		helpInvoke: "[-a] [-s macroset]",
		helpDesc:   "List all currently-defined macros in the current macroset. If -s is given, that macroset is shown in the output. -s can be given multiple times. -a includes all macrosets.",
		argsExec:   executeCommandList,
	},
	"SHOW": command{
		helpInvoke: "<macro>",
		helpDesc:   "Show the contents of a macro in the current macroset. Macro names are case-insensitive.",
		argsExec:   executeCommandShow,
	},
	"MACROSET": {
		helpInvoke: "[macroset_name] [-d]",
		helpDesc:   "Without arguments, gives the name of the current macroset. If a name is given, switches the current macroset to the given one, which makes all DEFINE calls made while that macroset was active also go inactive. All further DEFINES will then apply to the switched-to macroset. If the macroset did not already exist, it is created. If -d is given instead of a macroset name, the current macroset switches to the default one. Macroset names are case-insensitive.",
		argsExec:   executeCommandMacroset,
	},
	"RENAME": {
		helpInvoke: "<old_name OR -d> <new_name> [-m] [-s]",
		helpDesc:   "Renames the item referred to by old_name to new_name. The old_name must be either a macro created with DEFINE or a macroset created with MACROSET, or -d to specify the default macroset. If it is the name of both a macro and a macroset, either -m must be given to specify the DEFINE-created macro or -s must be given to specify the MACROSET-created macroset.",
		argsExec:   executeCommandRename,
	},
	"LISTSETS": {
		helpDesc: "Gives a list of all currently-loaded macrosets. Macrosets that do not currently contain any macro definitions will not be shown.",
		argsExec: executeCommandListsets,
	},
	"EXPORT": command{
		helpInvoke: "<filename> [-c] [-s macroset1 [... -s macrosetN]]",
		helpDesc:   "Exports the current macro definitions to the given filename, to be loaded via a later call to IMPORT or by giving the definitions file to use when launching netkk with --macrofile. By default the macros in all macrosets are included; this can be changed by giving any combination of -c and one or more -s options. Giving -c specifies the current macroset, and -m followed by the name of a macroset specifies that macroset.",
		argsExec:   executeCommandExport,
	},
	"IMPORT": command{
		helpInvoke: "<filename> [-r]",
		helpDesc:   "Imports macro definitions in the given file. By default they extend the ones already defined; if -r is given, all macrosets are cleared and removed before using the ones in the file.",
		argsExec:   executeCommandImport,
	},
}

type userInput struct {
	input        string
	inputForHist string
	promptErr    error
	connErr      error
}

func init() {
	// have to add this afterwards else we get into an initialization loop
	commands["HELP"] = command{
		interactiveOnly: true,
		helpDesc:        "Show this help",
		argsExec: func(state *consoleState, argv []string) (string, error) {
			return showHelp(), nil
		},
	}
}

func executeCommandClearhist(state *consoleState, args []string) (output string, err error) {
	if !state.interactive {
		return "", fmt.Errorf("%s command only available in interactive mode", args[0])
	}
	state.prompt.ClearHistory()
	if state.usingHistFile {
		state.usingHistFile = writeHistFile(state.prompt, state.out, "nkk")
	}
	output = state.out.InfoSprintf("Command history has been cleared")
	return output, nil
}

func autoComplete(language string, state *consoleState, line string) (candidates []string) {
	commandNames := commands.names()
	for _, word := range commandNames {
		if strings.HasPrefix(strings.ToLower(word), line) {
			candidates = append(candidates, strings.ToLower(word))
		}
		if strings.HasPrefix(strings.ToUpper(word), line) {
			candidates = append(candidates, strings.ToUpper(word))
		}
	}
	if len(candidates) == 0 {
		for _, word := range commandNames {
			if strings.HasPrefix(strings.ToUpper(word), strings.ToUpper(line)) {
				candidates = append(candidates, strings.ToUpper(word))
			}
		}
	}

	return candidates
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

func normalizeLine(line string) (result string, skipCommandMatching bool) {
	cmd := strings.SplitN(line, "#", 2)[0]
	cmd = strings.SplitN(cmd, "//", 2)[0]
	cmd = strings.TrimFunc(cmd, unicode.IsSpace)

	skipCommandMatching = false
	if strings.HasPrefix(cmd, ":>") {
		skipCommandMatching = true
		cmd = strings.SplitN(cmd, ":>", 2)[1]
		cmd = strings.TrimFunc(cmd, unicode.IsSpace)
	}
	return cmd, skipCommandMatching
}

func isCompleteLine(state *consoleState, line string) bool {
	cmd, skipCommandMatching := normalizeLine(line)

	if !skipCommandMatching {
		isCommand, _, _ := commands.parseCommand(cmd)
		if isCommand {
			return true
		}
	}

	return strings.HasSuffix(cmd, ";")
}

func parseLineToBytes(line string) (data []byte, err error) {

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
	normalLine, skipCommandMatching := normalizeLine(line)
	if state.delimitWithSemicolon {
		normalLine = strings.TrimSuffix(normalLine, ";")
	}

	if normalLine == "" {
		state.out.Trace("not sending empty escaped input\n")
		exitExpected = true
		return "", nil
	}

	if !skipCommandMatching {
		output, executed, err := commands.executeIfIsCommand(state, normalLine)
		if executed {
			exitExpected = true
			return output, err
		}
	}

	_, err = executeCommandSend(state, "SEND "+normalLine, "SEND")
	if err != nil {
		exitExpected = true
		return "", err
	}
	return "", nil
}

func executeCommandSend(state *consoleState, line string, cmdName string) (output string, err error) {
	var data []byte
	if len(line) != len(cmdName) {
		firstSpace := strings.IndexFunc("", unicode.IsSpace)
		if firstSpace <= -1 {
			state.out.Trace("shouldn't have gotten here in the parse, but should be okay; sending empty data as typical")
		} else {
			linePastCommand := strings.TrimSpace(line[firstSpace:])
			data, err = parseLineToBytes(linePastCommand)
			if err != nil {
				return "", err
			}
		}
	}
	return "", state.connection.Send(data)
}

func executeCommandDefine(state *consoleState, line string, cmdName string) (string, error) {
	parts := strings.Split(strings.TrimSpace(misc.CollapseWhitespace(line)), " ")
	if len(parts) < 2 {
		return "", fmt.Errorf("need to give name of macro to define")
	}
	if len(parts) < 3 {
		return "", fmt.Errorf("empty macros are not allowed; give contents of macro after name")
	}
	macroName := parts[1]

	// done checking args
	alreadyExists := state.macros.IsDefined(macroName)
	if err := state.macros.Define(macroName, strings.Join(parts[2:], " ")); err != nil {
		return "", err
	}
	if alreadyExists {
		return state.out.InfoSprintf("Updated %q to new contents", macroName), nil
	}
	return state.out.InfoSprintf("Defined new macro %q", macroName), nil
}

func executeCommandUndefine(state *consoleState, argv []string) (output string, err error) {
	var macroName string
	var doReplacement bool

	argv, err = parseCommandFlags(
		argv,
		flagActions{
			'r': func(i *int, argv []string) error {
				doReplacement = true
				return nil
			},
		},
		posArgActions{
			func(i *int, argv []string) error {
				macroName = argv[*i]
				return nil
			},
		},
	)
	if err != nil {
		return "", err
	}

	// if the current macroset doesn't yet exist, there's nothing to not define.
	if state.macros.Undefine(macroName, doReplacement) {
		return state.out.InfoSprintf("Deleted macro %q", macroName), nil
	}
	return state.out.InfoSprintf("%q is not currently a defined macro, so not doing anything", argv[1]), nil
}

func executeCommandList(state *consoleState, argv []string) (output string, err error) {
	var listAll bool
	includeSet := []string{}

	argv, err = parseCommandFlags(
		argv,
		flagActions{
			'a': func(i *int, argv []string) error {
				listAll = true
				return nil
			},
			'm': func(i *int, argv []string) error {
				if *i+1 >= len(argv) {
					return fmt.Errorf("argument required after -m")
				}
				includeSet = append(includeSet, argv[*i+1])

				// we have consumed an extra item, so bump up i and continue
				*i = *i + 1

				return nil
			},
		},
		nil,
	)
	if err != nil {
		return "", err
	}

	if listAll {
		includeSet = state.macros.GetSetNames()
	}

	var sb strings.Builder
	if len(includeSet) > 0 {
		for _, setName := range includeSet {
			if setName == "" {
				sb.WriteString("(default macroset):\n")
			} else {
				sb.WriteString("MACROSET ")
				sb.WriteString(setName)
				sb.WriteString(":\n")
			}
			names := state.macros.GetNamesIn(setName)
			if len(names) < 1 {
				sb.WriteString("  (none defined)\n")
			} else {
				for _, macro := range names {
					sb.WriteString("  ")
					sb.WriteString(macro)
					sb.WriteRune('\n')
				}
			}
			sb.WriteRune('\n')
		}
	} else {
		for _, mName := range state.macros.GetNames() {
			sb.WriteString(mName)
			sb.WriteRune('\n')
		}
	}

	return sb.String(), nil
}

func executeCommandShow(state *consoleState, argv []string) (output string, err error) {
	if len(argv) < 2 {
		return "", fmt.Errorf("need to give name of macro to show")
	}
	if !state.macros.IsDefined(argv[1]) {
		return "", fmt.Errorf("%q is not a defined macro", argv[1])
	}
	return state.macros.Get(argv[1]), nil
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
func ExecuteScript(f io.Reader, conn driver.Connection, out verbosity.OutputWriter, version string, delimitWithSemicolon bool) (lines int, err error) {
	state := &consoleState{connection: conn, version: version, out: out, interactive: false, delimitWithSemicolon: delimitWithSemicolon}
	scanner := bufio.NewScanner(f)
	lineNum := 0
	numLinesRead := 0
	moreInputRequired := true
	cmd := ""

	for scanner.Scan() {
		lineNum++
		partialCmd := scanner.Text()
		normalPartial, _ := normalizeLine(partialCmd)
		if normalPartial == "" {
			continue
		}
		cmd += normalPartial
		moreInputRequired = state.delimitWithSemicolon && !isCompleteLine(state, cmd)
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
func StartPrompt(conn driver.Connection, out verbosity.OutputWriter, version string, language string, delimitWithSemicolon bool, showPromptText bool) (err error) {

	state := consoleState{
		running:              true,
		connection:           conn,
		out:                  out,
		version:              version,
		interactive:          true,
		language:             language,
		delimitWithSemicolon: delimitWithSemicolon,
	}

	// sleep until ready
	for !state.connection.Ready() {
		time.Sleep(101 * time.Millisecond)
		if state.connection.IsClosed() {
			return fmt.Errorf("driver was closed before it became ready")
		}
	}

	state.setupConsoleLiner(language)
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
			state.setupConsoleLiner(language)
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
		if state.usingHistFile {
			state.usingHistFile = writeHistFile(state.prompt, state.out, state.language)
		}

		cmdOutput, err := executeLine(&state, cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			if state.connection.IsClosed() {
				state.running = false
			}
		} else if cmdOutput != "" {
			fmt.Printf("%s\n", cmdOutput)
		}
	}
	return nil
}

func (state *consoleState) setupConsoleLiner(language string) {
	state.prompt = liner.NewLiner()
	state.prompt.SetCtrlCAborts(true)
	state.prompt.SetMultiLineMode(true)

	state.prompt.SetCompleter(func(line string) []string {
		return autoComplete(language, state, line)
	})
	state.usingHistFile = loadHistFile(state.prompt, state.out, state.language)
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
		normalPartial, _ := normalizeLine(partialCmd)
		if normalPartial == "" {
			continue
		}
		cmd += normalPartial
		cmdWithSpaces += normalPartial

		moreInputRequired = state.delimitWithSemicolon && !isCompleteLine(state, cmd)
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

func loadHistFile(prompt *liner.State, out verbosity.OutputWriter, language string) bool {
	var histPath string
	homedir, err := os.UserHomeDir()
	if err != nil {
		out.Warn("couldn't get homedir; command history will be limited to this session: %v\n", err)
		return false
	}
	appDir := filepath.Join(homedir, ".netkk")
	err = os.Mkdir(appDir, os.ModeDir|0755)
	if err != nil && !os.IsExist(err) {
		out.Warn("couldn't create ~/.netkk; command history will be limited to this session: %v\n", err)
		return false
	}

	filename := fmt.Sprintf("history-%s", strings.ToLower(language))
	histPath = filepath.Join(appDir, filename)
	f, err := os.Open(histPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true
		}
		out.Warn("couldn't open ~/.netkk/%s; command history will be limited to this session: %v\n", filename, err)
		return false
	}
	defer f.Close()
	_, err = prompt.ReadHistory(f)
	if err != nil {
		out.Warn("couldn't read history file: %v\n", err)
	}
	return true
}

func writeHistFile(prompt *liner.State, out verbosity.OutputWriter, language string) bool {
	var histPath string
	homedir, err := os.UserHomeDir()
	if err != nil {
		out.Warn("couldn't get homedir; command history will be limited to this session: %v\n", err)
		return false
	}
	appDir := filepath.Join(homedir, ".netkk")
	err = os.Mkdir(appDir, os.ModeDir|0755)
	if err != nil && !os.IsExist(err) {
		out.Warn("couldn't create ~/.netkk; command history will be limited to this session: %v\n", err)
		return false
	}
	filename := fmt.Sprintf("history-%s", strings.ToLower(language))
	histPath = filepath.Join(appDir, filename)
	f, err := os.Create(histPath)
	if err != nil {
		out.Warn("couldn't open ~/.netkk/%s; command history will be limited to this session: %v\n", filename, err)
		return false
	}
	defer f.Close()
	_, err = prompt.WriteHistory(f)
	if err != nil {
		out.Warn("couldn't write history file: %v\n", err)
		return false
	}
	return true
}

func getSplashTextArt() []string {
	return []string{
		"   _______________________   ",
		"  /                       \\  ",
		" |    NETKARKAT, HUMAN!   | ",
		"  \\_______________________/  ",
	}
}

func printSplashTextArt(xCoord int, outputter verbosity.OutputWriter) {
	tabBytes := make([]byte, xCoord)
	for i := 0; i < len(tabBytes); i++ {
		tabBytes[i] = 0x20
	}
	tabs := string(tabBytes)

	outputter.Info("\n")
	for _, line := range getSplashTextArt() {
		outputter.Info("%s%s\n", tabs, line)
	}
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
