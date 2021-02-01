package console

import (
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/google/shlex"
)

func autoComplete(state *consoleState, line string) (candidates []string) {
	candidates = autoCompleteFilename(line)
	if candidates != nil {
		return candidates
	}
	candidates = autoCompleteCommand(line)
	macroCandidates := autoCompleteMacros(state, line)
	if macroCandidates != nil {
		candidates = append(candidates, macroCandidates...)
	}
	return candidates
}

func autoCompleteCommand(partial string) (candidates []string) {
	commandNames := commands.names()
	for _, word := range commandNames {
		if strings.HasPrefix(strings.ToLower(word), partial) {
			candidates = append(candidates, strings.ToLower(word))
		}
		if strings.HasPrefix(strings.ToUpper(word), partial) {
			candidates = append(candidates, strings.ToUpper(word))
		}
	}
	if len(candidates) == 0 {
		for _, word := range commandNames {
			if strings.HasPrefix(strings.ToUpper(word), strings.ToUpper(partial)) {
				candidates = append(candidates, strings.ToUpper(word))
			}
		}
	}
	return candidates
}

func autoCompleteMacros(state *consoleState, line string) []string {
	parts, err := shlex.Split(line)
	if err != nil {
		return nil
	}
	if len(parts) < 1 {
		return nil
	}

	var candidates []string
	for _, n := range state.macros.GetNames() {
		if strings.HasPrefix(n, parts[len(parts)-1]) {
			if len(parts) == 1 {
				candidates = append(candidates, n)
			} else {
				candidates = append(candidates, strings.Join(parts[:len(parts)-1], " ")+" "+n)
			}
		}
	}
	return candidates
}

func autoCompleteFilename(line string) []string {
	var candidates []string
	// check for import/export, do filesystem completions in that case
	parts, err := shlex.Split(line)
	if err == nil {
		if len(parts) == 2 {
			if strings.ToUpper(parts[0]) == "IMPORT" || strings.ToUpper(parts[0]) == "EXPORT" {
				// okay we're on the second item, try to do a filesystem completion.
				path := parts[1]
				fsDir := filepath.Dir(path)
				files, err := ioutil.ReadDir(fsDir)
				if err != nil {
					return nil
				}
				for _, fs := range files {
					if !fs.IsDir() && strings.HasPrefix(fs.Name(), parts[1]) {
						candidates = append(candidates, parts[0]+" "+fs.Name())
					}
				}
			}
		}
	}
	return candidates
}
