package console

import "fmt"

// parse the short args out of the given string. will always return none on empty or
// none on string that contains impossible characters
func parseShortOpts(s string) []rune {
	sargs := map[rune]bool{}
	for idx, ch := range s {
		if idx == 0 {
			if ch != '-' {
				return nil
			}
			continue
		}
		if ch == '-' {
			return nil
		}
		sargs[ch] = true
	}

	if len(sargs) == 0 {
		return nil
	}

	sargsSlice := []rune{}
	for k := range sargs {
		sargsSlice = append(sargsSlice, k)
	}
	return sargsSlice
}

type argParseHandler func(*int, []string) error
type argParsePosAction struct {
	parse    argParseHandler
	optional bool
}
type flagActions map[rune]argParseHandler
type posArgActions []argParsePosAction

func validatePosArgList(posActs posArgActions) (numRequired int, validationErr error) {
	numRequiredPosArgs := 0
	// cant have a non optional after the first optional; makes no sense.
	// make sure that is not happening
	hitOptional := false
	for _, posAction := range posActs {
		if hitOptional && !posAction.optional {
			return 0, fmt.Errorf("can't have required positional argument after optional one")
		} else if !hitOptional {
			if posAction.optional {
				hitOptional = true
			} else {
				numRequiredPosArgs++
			}
		}
	}
	return numRequiredPosArgs, nil
}

// it is valid to change the value of posArgActions via other flags/operations;
// optional calculation and "got number of required" is only done after parsing
// is complete, so it is valid for a function in a flagAction or posArgAction to
// change the properties of another.
func parseCommandFlags(argv []string, flags flagActions, args posArgActions) ([]string, error) {
	// do validation check; we don't care about num required until after
	if _, err := validatePosArgList(args); err != nil {
		return nil, fmt.Errorf("pre-parse positional arg actions error: %v", err)
	}

	var parsedArgs []string
	parsingOpts := true
	curPosItem := 0
	var arg string
	for i := 0; i < len(argv); i++ {
		if i == 0 {
			continue
		}
		arg = argv[i]
		if parsingOpts {
			if arg == "--" {
				parsingOpts = false
				continue
			} else {
				sargs := parseShortOpts(arg)
				if len(sargs) > 0 {
					for _, ch := range sargs {
						if flagHandler, ok := flags[ch]; ok {
							if err := flagHandler(&i, argv); err != nil {
								return parsedArgs, err
							}
						} else {
							return parsedArgs, fmt.Errorf("unknown option %q", ch)
						}
					}
					continue
				}
			}
		}
		if curPosItem >= len(args) {
			if len(args) == 0 {
				return parsedArgs, fmt.Errorf("unknown argument %q; command doesn't take any arguments", arg)
			} else if len(args) == 1 {
				return parsedArgs, fmt.Errorf("unknown argument %q; command only takes 1 argument", arg)
			} else {
				return parsedArgs, fmt.Errorf("unknown argument %q; command only takes %d arguments", arg, len(args))
			}
		}
		if err := args[curPosItem].parse(&i, argv); err != nil {
			return parsedArgs, err
		}
		parsedArgs = append(parsedArgs, arg)
		curPosItem++
	}

	// number of optional/non-optional may have changed during run, so validate
	// both no-required-after-first-optional AND do actual count of total required

	numRequiredPosArgs, err := validatePosArgList(args)
	if err != nil {
		return nil, fmt.Errorf("post-parse positional arg actions error: %v", err)
	}
	if curPosItem != numRequiredPosArgs {
		if numRequiredPosArgs == 1 {
			return parsedArgs, fmt.Errorf("expected at least 1 argument")
		}
		return parsedArgs, fmt.Errorf("expected at least %d arguments", numRequiredPosArgs)
	}
	return parsedArgs, nil
}
