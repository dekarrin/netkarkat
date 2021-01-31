package macros

import (
	"sort"
	"strings"
)

func (set macroset) replaceMacros(text string, macrosUsed map[string]bool) (parsed string, newMacrosUsed map[string]bool, err error) {
	// copy the macros used so we dont overwrite
	combinedMacrosUsed := make(map[string]bool, len(macrosUsed))
	for k := range macrosUsed {
		combinedMacrosUsed[k] = macrosUsed[k]
	}
	newMacrosUsed = make(map[string]bool)

	allMacros := set.GetAll()
	sort.Sort(sortableMacroList(allMacros))

	workingText := text

	// for each macro...
	for _, name := range allMacros {
		m := set.macros[strings.ToUpper(name)]
		matches := m.regex.FindAllStringIndex(workingText, -1)
		if matches == nil {
			continue
		}

		// if it is one we have seen, break out, we're in a cycle
	}
}
