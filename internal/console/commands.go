package console

import (
	"dekarrin/netkarkat/internal/misc"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/google/shlex"
)

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
		helpInvoke: "bytes...",
		helpDesc:   "Sends bytes. This command is assumed when no other command is given. It can be used to send literal bytes that would be otherwise interpreted as a command, such as `SEND LIST` to send the literal bytes that make up L, I, S, and T. It can also be used to explicitly instruct the console to perform a send of 0 bytes on the connection; whether this results in actual network traffic depends on the underlying driver.",
		lineExec:   executeCommandSend,
	},
	"DEFINE": command{
		helpInvoke: "macro bytes...",
		helpDesc:   "Create a macro that can be typed instead of a sequence of bytes; after DEFINE is used, the supplied name will be interpreted to be the supplied bytes in any context that takes bytes. Macros can also be used in other macro definitions, and will update the macro they are in when their own contents change. Macro names are case-insensitive.",
		lineExec:   executeCommandDefine,
	},
	"UNDEFINE": command{
		helpInvoke: "[-r] macro",
		helpDesc:   "Remove the definition of an existing macro created in a previous call to DEFINE. By default, any other macros that included the removed macro in their definitions will simply keep them as the bytes that represent the characters in the deleted macro's name; to have them replace it with its previous contents and continue to function as before, give the -r flag. Macro names are case-insensitive.",
		argsExec:   executeCommandUndefine,
	},
	"LIST": command{
		helpInvoke: "[-a] [-s macroset]",
		helpDesc:   "List all currently-defined macros in the current macroset. If -s is given, that macroset is shown in the output. -s can be given multiple times. -a includes all macrosets.",
		argsExec:   executeCommandList,
	},
	"SHOW": command{
		helpInvoke: "macro",
		helpDesc:   "Show the contents of a macro in the current macroset. Macro names are case-insensitive.",
		argsExec:   executeCommandShow,
	},
	"MACROSET": {
		helpInvoke: "[-d] [name]",
		helpDesc:   "Without arguments, gives the name of the current macroset. If a name is given, switches the current macroset to the given one, which makes all DEFINE calls made while that macroset was active also go inactive. All further DEFINES will then apply to the switched-to macroset. If the macroset did not already exist, it is created. If -d is given instead of a macroset name, the current macroset switches to the default one. Macroset names are case-insensitive.",
		argsExec:   executeCommandMacroset,
	},
	"RENAME": {
		helpInvoke: "[-rmsd] old new",
		helpDesc:   "Renames the item referred to by old name to new name. The old name must be either a macro created with DEFINE or a macroset created with MACROSET, or -d to specify the default macroset. If old name is the name of both a macro and a macroset, either -m must be given to specify the DEFINE-created macro or -s must be given to specify the MACROSET-created macroset. If a macro is being renamed and -r is given, its usage will be replaced with its new name in all other macros that refer to it.",
		argsExec:   executeCommandRename,
	},
	"LISTSETS": {
		helpDesc: "Gives a list of all currently-loaded macrosets. Macrosets that do not currently contain any macro definitions will not be shown.",
		argsExec: executeCommandListsets,
	},
	"EXPORT": command{
		helpInvoke: "[-c] [-s macroset] file",
		helpDesc:   "Exports the current macro definitions to the given filename, to be loaded via a later call to IMPORT or by giving the definitions file to use when launching netkk with --macrofile. By default the macros in all macrosets are included; this can be changed by giving any combination of -c and one or more -s options. Giving -c specifies the current macroset, and -m followed by the name of a macroset specifies that macroset.",
		argsExec:   executeCommandExport,
	},
	"IMPORT": command{
		helpInvoke: "[-r] file",
		helpDesc:   "Imports macro definitions in the given file. By default they extend the ones already defined; if -r is given, all macrosets are cleared and removed before using the ones in the file.",
		argsExec:   executeCommandImport,
	},
}

// called by init() function
func initCommands() {
	// have to add this afterwards else we get into an initialization loop
	commands["HELP"] = command{
		interactiveOnly: true,
		helpInvoke:      " [command]",
		helpDesc:        "Show this help. If command is given, shows only help on that particular command.",
		argsExec: func(state *consoleState, argv []string) (string, error) {
			if len(argv) >= 2 {
				return showHelp(argv[1]), nil
			}
			return showHelp(""), nil
		},
	}
}

func executeCommandClearhist(state *consoleState, args []string) (output string, err error) {
	if !state.interactive {
		return "", fmt.Errorf("%s command only available in interactive mode", args[0])
	}
	state.prompt.ClearHistory()
	state.writeHistFile()
	output = state.out.InfoSprintf("Command history has been cleared")
	return output, nil
}

func executeCommandSend(state *consoleState, line string, cmdName string) (output string, err error) {
	var data []byte
	if len(line) != len(cmdName) {
		firstSpace := strings.IndexFunc(line, unicode.IsSpace)
		if firstSpace <= -1 {
			state.out.Trace("being told to send empty string; skipping line parse")
		} else {
			linePastCommand := strings.TrimSpace(line[firstSpace:])
			data, err = state.parseLineToBytes(linePastCommand)
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
	if state.usingUserPersistenceFiles {
		state.writeMacrosFile()
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
			{
				parse: func(i *int, argv []string) error {
					macroName = argv[*i]
					return nil
				},
			},
		},
	)
	if err != nil {
		return "", err
	}

	// if the current macroset doesn't yet exist, there's nothing to not define.
	if state.macros.Undefine(macroName, doReplacement) {
		if state.usingUserPersistenceFiles {
			state.writeMacrosFile()
		}
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
		names := state.macros.GetNames()
		if len(names) < 1 {
			sb.WriteString("(none defined)")
		} else {
			for _, mName := range names {
				sb.WriteString(mName)
				sb.WriteRune('\n')
			}
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

func executeCommandMacroset(state *consoleState, argv []string) (output string, err error) {
	var swapToDefault bool
	var swapTo string
	argv, err = parseCommandFlags(
		argv,
		flagActions{
			'd': func(i *int, argv []string) error {
				swapToDefault = true
				return nil
			},
		},
		posArgActions{
			{
				parse: func(i *int, argv []string) error {
					if argv[*i] == "" {
						return fmt.Errorf("blank macroset name is not allowed; use -d to switch to the default macroset")
					}
					swapTo = argv[*i]
					return nil
				},
				optional: true,
			},
		},
	)
	if err != nil {
		return "", err
	}

	if swapTo != "" && swapToDefault {
		return "", fmt.Errorf("both -d and a macroset name were given; only one is allowed")
	}

	if swapToDefault {
		if err := state.macros.SetCurrentMacroset(""); err != nil {
			return "", err
		}
		return state.out.InfoSprintf("Switched current macroset to the default one."), nil
	} else if swapTo != "" {
		if err := state.macros.SetCurrentMacroset(swapTo); err != nil {
			return "", err
		}
		return state.out.InfoSprintf("Switched current macroset to %q.", swapTo), nil
	}

	// and the last case, no args, user just wants to know the current one.
	// do not mask behind verbosity as user specifically requested this and it should
	// show even in the queitest of modes.
	curSetName := state.macros.GetCurrentMacroset()
	if curSetName == "" {
		return "(default macroset)", nil
	}
	return curSetName, nil
}

func executeCommandRename(state *consoleState, argv []string) (output string, err error) {
	// "[-m OR -s] <old_name OR -d> <new_name>"

	var isMacro, isSet, isDefaultSet, doReplacement bool
	var firstName, secondName string

	posArgs := posArgActions{
		// oldName:
		{
			parse: func(i *int, argv []string) error {
				if argv[*i] == "" {
					return fmt.Errorf("blank name is not allowed; use -d if attempting to specify the default macroset")
				}
				firstName = argv[*i]
				return nil
			},
		},

		// newName:
		{
			parse: func(i *int, argv []string) error {
				if argv[*i] == "" {
					return fmt.Errorf("blank new name is not allowed")
				}
				secondName = argv[*i]
				return nil
			},
		},
	}

	argv, err = parseCommandFlags(
		argv,
		flagActions{
			'm': func(i *int, argv []string) error {
				if isDefaultSet {
					return fmt.Errorf("-d implies -s; cannot also give -m")
				}
				if isSet {
					return fmt.Errorf("cannot set both -s and -m; select one")
				}
				isMacro = true
				return nil
			},
			'r': func(i *int, argv []string) error {
				doReplacement = true
				return nil
			},
			's': func(i *int, argv []string) error {
				if isMacro {
					return fmt.Errorf("cannot set both -s and -m; select one")
				}
				isSet = true
				return nil
			},
			'd': func(i *int, argv []string) error {
				if isMacro {
					return fmt.Errorf("-d implies -s; cannot also give -m")
				}
				isSet = true
				isDefaultSet = true

				// this also makes "new name" optional; the "old name" is actually
				// going to be the new name in this case.
				posArgs[1] = argParsePosAction{
					parse:    posArgs[1].parse,
					optional: true,
				}

				return nil
			},
		},
		posArgs,
	)
	if err != nil {
		return "", err
	}

	if isDefaultSet {
		if doReplacement {
			return "", fmt.Errorf("-r can only be given for macros, not sets")
		}
		err := state.macros.RenameSet("", firstName)
		if err != nil {
			return "", err
		}

		if state.usingUserPersistenceFiles {
			state.writeMacrosFile()
		}
		return state.out.InfoSprintf("Saved the current default set to new name %q", firstName), nil
	}

	// if user has not specified whether macro or set, need to do more work to decide
	if !isSet && !isMacro {
		isMacro = state.macros.IsDefined(firstName)
		isSet = state.macros.IsDefinedMacroset(firstName)
		if isMacro && isSet {
			return "", fmt.Errorf("%q refers to both a macroset and to a macro in the current macroset; specify which with -s or -m", firstName)
		}
		if !isMacro && !isSet {
			return "", fmt.Errorf("there is not currently any macroset or macro called %q", firstName)
		}
	}

	// okay by now it either is a macro or a macroset
	if isSet {
		if doReplacement {
			return "", fmt.Errorf("-r can only be given for macros, not sets")
		}
		err := state.macros.RenameSet(firstName, secondName)
		if err != nil {
			return "", err
		}
		return state.out.InfoSprintf("Renamed macroset %q to %q", firstName, secondName), nil
	} else if isMacro {
		err := state.macros.Rename(firstName, secondName, doReplacement)
		if err != nil {
			return "", err
		}
		msg := "Renamed macro %q to %q"
		if doReplacement {
			msg += " and updated all usages in other macros to match"
		}
		if state.usingUserPersistenceFiles {
			state.writeMacrosFile()
		}
		return state.out.InfoSprintf(msg, firstName, secondName), nil
	}

	// should never get here
	return "", fmt.Errorf("neither -m nor -s specified and autodetection is incomplete")
}

func executeCommandListsets(state *consoleState, argv []string) (output string, err error) {
	var sb strings.Builder
	names := state.macros.GetSetNames()
	for _, n := range names {
		if n == "" {
			sb.WriteString("(default macroset)\n")
		} else {
			sb.WriteString(n)
			sb.WriteRune('\n')
		}
	}
	return sb.String(), nil
}

func executeCommandImport(state *consoleState, argv []string) (output string, err error) {
	var importFile *os.File
	var doReplace bool
	argv, err = parseCommandFlags(
		argv,
		flagActions{
			'r': func(i *int, argv []string) error {
				doReplace = true
				return nil
			},
		},
		posArgActions{
			{
				parse: func(i *int, argv []string) error {
					f, err := os.Open(argv[*i])
					if err != nil {
						return fmt.Errorf("could not import file: %v", err)
					}
					importFile = f
					return nil
				},
			},
		},
	)
	if importFile != nil {
		defer importFile.Close()
	}
	if err != nil {
		return "", err
	}

	successFmt := "Loaded %d total macro%s in %d macroset%s"
	if doReplace {
		state.macros.Clear()
		successFmt = "Replaced all macros with %d total macro%s in %d macroset%s"
	}
	setCount, macroCount, err := state.macros.Import(importFile)
	if err != nil {
		return "", err
	}

	setS := "s"
	macroS := "s"
	if setCount == 1 {
		setS = ""
	}
	if macroCount == 1 {
		macroS = ""
	}

	if state.usingUserPersistenceFiles {
		state.writeMacrosFile()
	}

	return state.out.InfoSprintf(successFmt, macroCount, macroS, setCount, setS), nil
}

func executeCommandExport(state *consoleState, argv []string) (output string, err error) {
	//"<filename> [-c] [-s macroset1 [... -s macrosetN]]",

	var exportFile *os.File
	includeSet := make(map[string]bool)
	argv, err = parseCommandFlags(
		argv,
		flagActions{
			'c': func(i *int, argv []string) error {
				includeSet[state.macros.GetCurrentMacroset()] = true
				return nil
			},
			's': func(i *int, argv []string) error {
				if *i+1 >= len(argv) {
					return fmt.Errorf("-s requires an argument")
				}
				*i++
				if argv[*i] == "" {
					return fmt.Errorf("-s requires a non-empty argument")
				}
				includeSet[argv[*i]] = true
				return nil
			},
		},
		posArgActions{
			{
				parse: func(i *int, argv []string) error {
					f, err := os.Create(argv[*i])
					if err != nil {
						return fmt.Errorf("could not import file: %v", err)
					}
					exportFile = f
					return nil
				},
			},
		},
	)
	if exportFile != nil {
		defer exportFile.Close()
	}
	if err != nil {
		return "", err
	}

	var totalSets, totalMacros int
	if len(includeSet) > 0 {
		includedMacrosets := []string{}
		for k := range includeSet {
			includedMacrosets = append(includedMacrosets, k)
		}
		sort.Strings(includedMacrosets)

		for _, macrosetName := range includedMacrosets {
			if state.macros.IsDefinedMacroset(macrosetName) {
				setCount, macroCount, err := state.macros.ExportSet(macrosetName, exportFile)
				if err != nil {
					return "", err
				}
				totalSets += setCount
				totalMacros += macroCount
			}
		}
	} else {
		var err error
		totalSets, totalMacros, err = state.macros.Export(exportFile)
		if err != nil {
			return "", err
		}
	}

	macroS := "s"
	setS := "s"

	if totalSets == 1 {
		setS = ""
	}
	if totalMacros == 1 {
		macroS = ""
	}

	message := "Wrote %d total macro%s in %d macroset%s"
	return state.out.InfoSprintf(message, totalMacros, macroS, totalSets, setS), nil
}
