package macros

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var (
	identifierRegex = regexp.MustCompile(`^[A-Za-z$_][A-Za-z$_0-9]*$`)
)

type macro struct {
	name    string
	content string
	regex   *regexp.Regexp
}

type macroset map[string]macro

// IsDefined returns whether the given macro is defined in the current
// macroset. Macro is case-insensitive
func (set macroset) IsDefined(macro string) bool {
	if set == nil {
		return false
	}
	_, ok := set[strings.ToUpper(macro)]
	return ok
}

// Export exports all macro definitions to the given writer.
func (set macroset) Export(w io.Writer) error {
	if set == nil {
		return nil
	}

	bufW := bufio.NewWriter(w)

	// alphabetize them
	macroNames := set.GetAll()
	for _, name := range macroNames {
		if _, err := bufW.WriteString(name); err != nil {
			return err
		}
		if _, err := bufW.WriteRune(' '); err != nil {
			return err
		}
		if _, err := bufW.WriteString(set.Get(name)); err != nil {
			return err
		}
		if _, err := bufW.WriteRune('\n'); err != nil {
			return err
		}
	}

	if err := bufW.Flush(); err != nil {
		return err
	}

	return nil
}

// Clear removes all current macros.
func (set macroset) Clear() {
	for _, k := range set.GetAll() {
		set.Undefine(k, false)
	}
}

// Import reads all macro definitions from the given reader. They are added to the current
// set, with any conflicting names replacing the original.
func (set macroset) Import(r io.Reader) error {
	scan := bufio.NewScanner(r)

	lineNo := 0
	for scan.Scan() {
		lineNo++
		line := scan.Text()
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 1 {
			return fmt.Errorf("malformed macro definition on line %q", lineNo)
		}
		name := parts[0]
		var content string
		if len(parts) >= 2 {
			content = parts[1]
		}
		if err := set.Define(name, content); err != nil {
			return fmt.Errorf("bad macro on line %q: %v", lineNo, err)
		}
	}
	if err := scan.Err(); err != nil {
		return fmt.Errorf("problem reading input: %v", err)
	}
	return nil
}

// Get gets the contents of the given macro. If it is not defined, empty string
// is returned. Macro name is not case sensitive.
func (set macroset) Get(macro string) string {
	if !set.IsDefined(macro) {
		return ""
	}
	return set[strings.ToUpper(macro)].content
}

// GetAll gets a list of all currently-defined macros.
func (set macroset) GetAll() []string {
	list := []string{}
	for _, macro := range set {
		list = append(list, macro.name)
	}
	sort.Strings(list)
	return list
}

// Rename changes the name of a macro from one definition to another. If replace is given,
// also updates all usages of the macro's name in all other macros to match.
func (set macroset) Rename(oldName string, newName string, replace bool) error {
	if set == nil || !set.IsDefined(oldName) {
		return fmt.Errorf("no macro named %q exists", oldName)
	}
	if !identifierRegex.MatchString(newName) {
		return fmt.Errorf("%q is not a valid macro name", newName)
	}

	if replace {
		set.replaceAllMacro(oldName, newName)
	}

	oldMacro := set[strings.ToUpper(oldName)]
	if err := set.Define(newName, oldMacro.content); err != nil {
		return err
	}
	set.Undefine(oldName, false)
	return nil
}

// Define creates a new definition for a macro of the given name. The name is
// case-insensitive.
func (set macroset) Define(name string, content string) error {
	if !identifierRegex.MatchString(name) {
		return fmt.Errorf("%q is not a valid macro name", name)
	}
	if set == nil {
		panic("cant define on a nil macroset")
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("empty macros are not allowed; use UNDEFINE if you are trying to remove the macro")
	}
	newMacro := macro{
		name:    name,
		content: content,
		regex:   regexp.MustCompile(`(?i)\b` + strings.ReplaceAll(name, "$", `\$`) + `\b`),
	}
	if newMacro.regex.MatchString(newMacro.content) {
		return fmt.Errorf("content includes the macro itself; this is a circular definition")
	}
	set[strings.ToUpper(name)] = newMacro
	return nil
}

// Undefine removes a definition for a macro of the given name. The name is
// case-insensitive. If replace is set to true, all macros that currently
// reference this one will be replaced with the contents of the macro before
// it is deleted.
//
// If the one to be undefined does not exist, false is returned.
func (set macroset) Undefine(macro string, replace bool) (existed bool) {
	if set == nil {
		panic("cant undefine on a nil macroset")
	}
	if set.IsDefined(macro) {
		if replace {
			set.replaceAllMacro(macro, set[strings.ToUpper(macro)].content)
		}
		delete(set, strings.ToUpper(macro))
		return true
	}
	return false
}

func (set macroset) replaceAllMacro(name string, replacement string) {
	if set == nil {
		return
	}
	staleMacro, ok := set[strings.ToUpper(name)]
	if !ok {
		return
	}

	allMacroNames := []string{}
	for nameUpper := range set {
		allMacroNames = append(allMacroNames, nameUpper)
	}
	for _, nameUpper := range allMacroNames {
		if nameUpper == strings.ToUpper(name) {
			continue
		}
		oldMacro := set[nameUpper]
		newContent := staleMacro.regex.ReplaceAllString(oldMacro.content, replacement)
		set[nameUpper] = macro{
			name:    oldMacro.name,
			content: newContent,
			regex:   oldMacro.regex,
		}
	}
}

// MacroCollection is a collection of macros that
// are replaced with simple replacement.
// Macros can be embedded in other macros and will be fully scanned until there
// are no macros to substitute.
//
// The zero value for MacroCollection is ready to be used.
type MacroCollection struct {
	cur  string
	sets map[string]macroset
}

// IsDefined returns whether the given macro is defined in the current
// macroset.
func (mc MacroCollection) IsDefined(macro string) bool {
	if mc.sets == nil {
		return false
	}
	if set, ok := mc.sets[mc.cur]; ok {
		return set.IsDefined(macro)
	}
	return false
}

// Define creates a new definition for a macro of the given name in the current macroset. The name is
// case-insensitive.
func (mc *MacroCollection) Define(macro, content string) error {
	if mc.sets == nil {
		mc.sets = make(map[string]macroset)
	}
	if mc.sets[mc.cur] == nil {
		mc.sets[mc.cur] = make(macroset)
	}
	return mc.sets[mc.cur].Define(macro, content)
}

// Undefine removes a definition for a macro of the given name in the current
// macroset. The name is case-insensitive. If replace is set to true, all
// macros that currently reference this one will be replaced with the contents
// of the macro before it is deleted.
func (mc *MacroCollection) Undefine(macro string, replace bool) bool {
	if mc.sets == nil {
		return false
	}
	if mc.sets[mc.cur] == nil {
		return false
	}
	return mc.sets[mc.cur].Undefine(macro, replace)
}

// GetNames gives a list of all macro names in the current set.
func (mc *MacroCollection) GetNames() []string {
	if mc.sets == nil {
		return nil
	}
	if mc.sets[mc.cur] == nil {
		return nil
	}
	return mc.sets[mc.cur].GetAll()
}

// GetNamesIn gives a list of all macro names in the given set.
func (mc *MacroCollection) GetNamesIn(setName string) []string {
	if mc.sets == nil {
		return nil
	}

	if mc.sets[strings.ToUpper(setName)] == nil {
		return nil
	}
	return mc.sets[strings.ToUpper(setName)].GetAll()
}

// GetSetNames gives a list of all defined macroset names with at least one item each.
func (mc *MacroCollection) GetSetNames() []string {
	names := []string{}
	if mc.sets == nil {
		return nil
	}
	if mc.sets[mc.cur] == nil {
		return nil
	}
	return mc.sets[mc.cur].GetAll()
}