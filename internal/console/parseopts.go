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
type flagActions map[rune]argParseHandler
type posArgActions []argParseHandler

func parseCommandFlags(argv []string, flags flagActions, args posArgActions) ([]string, error) {
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
		if err := args[curPosItem](&i, argv); err != nil {
			return parsedArgs, err
		}
		parsedArgs = append(parsedArgs, arg)
		curPosItem++
	}
	if curPosItem != len(args) {
		if len(args) == 1 {
			return parsedArgs, fmt.Errorf("expected at least 1 argument")
		}
		return parsedArgs, fmt.Errorf("expected at least %d arguments", len(args))
	}
	return parsedArgs, nil
}
