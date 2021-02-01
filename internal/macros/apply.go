package macros

import (
	"dekarrin/netkarkat/internal/stack"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

type sortableMacroList []string

func (a sortableMacroList) Len() int {
	return len(a)
}

func (a sortableMacroList) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a sortableMacroList) Less(i, j int) bool {
	// we want descending order, so "less" in terms of list order will actually be the one
	// that is "more" in terms of content (length).

	firstWordRuneCount := utf8.RuneCountInString(a[i])
	secondWordRuneCount := utf8.RuneCountInString(a[j])
	if firstWordRuneCount != secondWordRuneCount {
		return firstWordRuneCount > secondWordRuneCount
	}

	// they are both equal in length, so give the one that comes last in the
	// alphabet. comparison must be case-insensitive
	return strings.ToUpper(a[i]) > strings.ToUpper(a[j])
}

// Apply does replacement of all applicable macros in the set to the given text.
// If a loop is detected, the process aborts.
//
// Each macro is evaluated when encountered, and the macro in text is replaced
// with the defined content. If the defined content contains further macros,
// they will be evaluated first, and this process repeates recursively. If at
// any point during a recursion a macro is encountered that has already been
// encountered, it is considered a loop, and the replacement will immediately
// terminate.
func (set macroset) Apply(text string) (string, error) {
	var stack stack.StringStack
	stack.Normalize = strings.ToUpper

	replaced, err := set.executeMacros(text, &stack)
	if err != nil {
		return "", err
	}
	return replaced, err
}

// Apply does replacement of all available macros. Returns an error if a loop is
// detected.
func (mc MacroCollection) Apply(text string) (replaced string, err error) {
	if mc.sets == nil {
		return text, nil
	}
	if set, ok := mc.sets[mc.cur]; ok {
		return set.Apply(text)
	}
	return text, nil
}

// returns true if there is a loop for the given case-insensitive macro name
// returns false if the given macro is not a currently defined macro.
func (set macroset) causesLoop(macro string) bool {
	if set.IsDefined(macro) {
		stack := stack.StringStack{Normalize: strings.ToUpper}
		_, err := set.executeMacros(strings.ToUpper(macro), &stack)
		return err != nil
	}
	return false
}

func (set macroset) executeMacros(text string, macrosUsed *stack.StringStack) (parsed string, err error) {
	allMacros := set.GetAll()

	// we must go through in length order, descending.
	// otherwise longer words would get obscured by them containing
	// a macro inside of them (e.g. we need to evaluate a macro called
	// "OrgTeam" before we evaluate a macro called "Org" or "Team".
	//
	// EDIT: the above will probably not apply since we are using a regex
	// with \b at both ends to find the macros. Do the sort anyways because
	// it is good defensive coding and it shouldn't have issues with
	// runtime at any reasonable number of macros.
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
		if macrosUsed.Contains(name) {
			return "", fmt.Errorf("macro %q includes itself in a loop", name)
		}

		macrosUsed.Push(name)
		replacement, err := set.executeMacros(m.content, macrosUsed)
		if err != nil {
			return "", err
		}
		macrosUsed.Pop()

		var sb strings.Builder
		var beforeStart, beforeEnd, afterStart, afterEnd, mStart, mEnd int
		for idx, match := range matches {
			mStart, mEnd = match[0], match[1]

			beforeEnd = int(math.Max(0, float64(mStart)-1))
			afterEnd = len(workingText)
			if idx+1 < len(matches) {
				afterEnd = matches[idx+1][0]
			}
			afterStart = int(math.Min(float64(afterEnd), float64(mEnd)+1))

			sb.WriteString(workingText[beforeStart:beforeEnd])
			sb.WriteString(replacement)
			sb.WriteString(workingText[afterStart:afterEnd])

			beforeStart = afterEnd
		}
		workingText = sb.String()
	}
	return workingText, nil
}
