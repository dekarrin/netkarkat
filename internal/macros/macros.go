package macros

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMinLength is the fewest number of characters that a
	// macro or macroset name is allowed to have. This is utf-8 safe, and specifies
	// number of runes, not number of bytes.
	DefaultMinLength = 3
)

var (
	identifierRegex = regexp.MustCompile(`^[A-Za-z$_][A-Za-z$_0-9]*$`)
	setSectionRegex = regexp.MustCompile(`^\[[A-Za-z$_][A-Za-z$_0-9]*\]$`)
)

// objectTypeName just gives what is shown in error message if not valid.
// does not refer to any golang typing info, i'm just bad at naming things
func validateName(name string, objectTypeName string, customMinLen int) error {
	if objectTypeName != "" {
		objectTypeName = strings.TrimSpace(objectTypeName) + " "
	}

	if !identifierRegex.MatchString(name) {
		return fmt.Errorf("%q is not a valid %sname", name, objectTypeName)
	}
	if utf8.RuneCountInString(name) < customMinLen {
		return fmt.Errorf("%snames must be at least %d characters", objectTypeName, customMinLen)
	}
	return nil
}

type macro struct {
	name    string
	content string
	regex   *regexp.Regexp
}

type macroset struct {
	name   string
	macros map[string]macro

	// MinLength is the same as MinLength in MacroCollection.
	MinLength int
}

// Len returns the number of currently defined macros.
func (set macroset) Len() int {
	return len(set.macros)
}

// IsDefined returns whether the given macro is defined in the current
// macroset. Macro is case-insensitive
func (set macroset) IsDefined(macro string) bool {
	if set.macros == nil {
		return false
	}
	_, ok := set.macros[strings.ToUpper(macro)]
	return ok
}

// Gets the name of the macroset
func (set macroset) GetName() string {
	return set.name
}

// gives the minimum length if set on the macroset; else returns
// the default.
func (set macroset) getMinLength() int {
	if set.MinLength > 0 {
		return set.MinLength
	}
	return DefaultMinLength
}

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
	if set.macros == nil {
		return text, nil
	}
	// we must go through in length order, descending.
	// otherwise longer words would get obscured by them containing
	// a macro inside of them (e.g. we need to evaluate a macro called
	// "OrgTeam" before we evaluate a macro called "Org" or "Team".
	//
	// EDIT: the above will probably not apply since we are using a regex
	// with \b at both ends to find the macros. Do the sort anyways because
	// it is good defensive coding and it shouldn't have issues with
	// runtime at any reasonable number of macros.
	allMacros := set.GetAll()
	sort.Sort(sortableMacroList(allMacros))

	workingText := text

	// for each macro...
	for _, name := range allMacros {
		m := set.macros[strings.ToUpper(name)]

		// for each match of the macro found...
		for idx, match := range m.regex.FindAllStringIndex(workingText, -1) {
			newText := m.content

		}
	}
	/*
		A = B hello    // valid definition
		B = A hello    // valid definition

		using A:
		"this is A result"
		-> "this is B hello result"
		pass 2
		-> "this is A hello hello result"

		for each replacement: fully run through it and see if we get a macro
		already encountered. if we do, that is a fucking problem.
	*/
}

// Sets the name of the macroset
func (set *macroset) SetName(name string) error {
	if err := validateName(name, "macroset", set.getMinLength()); err != nil {
		return err
	}

	set.name = name

	return nil
}

// Export exports all macro definitions to the given writer.
func (set macroset) Export(w io.Writer) error {
	if set.macros == nil {
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
func (set *macroset) Clear() {
	for _, k := range set.GetAll() {
		set.Undefine(k, false)
	}
}

// Import reads all macro definitions from the given reader. They are added to the current
// set, with any conflicting names replacing the original.
func (set *macroset) Import(r io.Reader) error {
	scan := bufio.NewScanner(r)

	lineNo := 0
	for scan.Scan() {
		lineNo++
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		name, content, err := parseMacroImportLine(line)
		if err != nil {
			return err
		}
		if err := set.Define(name, content); err != nil {
			return err
		}
	}
	if err := scan.Err(); err != nil {
		return fmt.Errorf("problem reading input: %v", err)
	}
	return nil
}

func parseMacroImportLine(line string) (name string, content string, err error) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 1 {
		return "", "", fmt.Errorf("blank definition not allowed")
	}
	name = parts[0]
	if len(parts) >= 2 {
		content = parts[1]
	}
	return name, content, nil
}

// Get gets the contents of the given macro. If it is not defined, empty string
// is returned. Macro name is not case sensitive.
func (set macroset) Get(macro string) string {
	if !set.IsDefined(macro) {
		return ""
	}
	return set.macros[strings.ToUpper(macro)].content
}

// GetAll gets a list of all currently-defined macros.
func (set macroset) GetAll() []string {
	if set.macros == nil {
		return nil
	}
	list := []string{}
	for _, macro := range set.macros {
		list = append(list, macro.name)
	}
	sort.Strings(list)
	return list
}

// Rename changes the name of a macro from one definition to another. If replace is given,
// also updates all usages of the macro's name in all other macros to match.
func (set *macroset) Rename(oldName string, newName string, replace bool) error {
	if set.macros == nil || !set.IsDefined(oldName) {
		return fmt.Errorf("no macro named %q exists", oldName)
	}
	if err := validateName(newName, "macro", set.getMinLength()); err != nil {
		return err
	}

	if replace {
		set.replaceAllMacro(oldName, newName)
	}

	oldMacro := set.macros[strings.ToUpper(oldName)]
	if err := set.Define(newName, oldMacro.content); err != nil {
		return err
	}
	set.Undefine(oldName, false)
	return nil
}

// Define creates a new definition for a macro of the given name. The name is
// case-insensitive.
func (set *macroset) Define(name string, content string) error {
	if err := validateName(name, "macro", set.getMinLength()); err != nil {
		return err
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
		return fmt.Errorf("content includes the macro itself; circular definitions are not allowed")
	}

	if set.macros == nil {
		set.macros = make(map[string]macro)
	}
	set.macros[strings.ToUpper(name)] = newMacro
	return nil
}

// Undefine removes a definition for a macro of the given name. The name is
// case-insensitive. If replace is set to true, all macros that currently
// reference this one will be replaced with the contents of the macro before
// it is deleted.
//
// If the one to be undefined does not exist, false is returned.
func (set *macroset) Undefine(macro string, replace bool) (existed bool) {
	if set == nil {
		panic("cant undefine on a nil macroset")
	}
	if set.IsDefined(macro) {
		if replace {
			set.replaceAllMacro(macro, set.macros[strings.ToUpper(macro)].content)
		}
		delete(set.macros, strings.ToUpper(macro))
		return true
	}
	return false
}

func (set *macroset) replaceAllMacro(name string, replacement string) {
	if set == nil {
		return
	}
	if set.macros == nil {
		return
	}
	staleMacro, ok := set.macros[strings.ToUpper(name)]
	if !ok {
		return
	}

	allMacroNames := []string{}
	for nameUpper := range set.macros {
		allMacroNames = append(allMacroNames, nameUpper)
	}
	for _, nameUpper := range allMacroNames {
		if nameUpper == strings.ToUpper(name) {
			continue
		}
		oldMacro := set.macros[nameUpper]
		newContent := staleMacro.regex.ReplaceAllString(oldMacro.content, replacement)
		set.macros[nameUpper] = macro{
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
	// cur is always upper-cased
	cur string

	sets map[string]macroset

	// MinLength is the minimum number of characters a macro or macroset name is allowed
	// to be. If set to 0, it falls back to the default of DefaultMinLength in the macro
	// package.
	MinLength int
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
	return mc.DefineIn(mc.GetCurrentMacroset(), macro, content)
}

// DefineIn creates a new definition for a macro of the given name in the given
// macroset. The names are case-insensitive. If the macroset doesn't yet exist,
// it is created. The current macroset remains unchanged.
func (mc *MacroCollection) DefineIn(setName, macroName, content string) error {
	if mc.sets == nil {
		mc.sets = make(map[string]macroset)
		mc.sets[""] = macroset{
			MinLength: mc.MinLength,
		}
	}

	var set macroset
	if existingSet, ok := mc.sets[strings.ToUpper(setName)]; ok {
		set = existingSet
	} else {
		set = macroset{
			name:      setName,
			MinLength: mc.MinLength,
		}
	}

	if err := set.Define(macroName, content); err != nil {
		return err
	}
	mc.sets[strings.ToUpper(setName)] = set
	return nil
}

// Get gets the contents of a macro. The name is case insensitive.
// If the macro does not exist, the empty string is returned.
func (mc *MacroCollection) Get(macro string) string {
	if !mc.IsDefined(macro) {
		return ""
	}
	return mc.sets[mc.cur].Get(macro)
}

// Undefine removes a definition for a macro of the given name in the current
// macroset. The name is case-insensitive. If replace is set to true, all
// macros that currently reference this one will be replaced with the contents
// of the macro before it is deleted.
func (mc *MacroCollection) Undefine(macro string, replace bool) bool {
	if mc.sets == nil {
		return false
	}
	var set macroset
	if existingSet, ok := mc.sets[mc.cur]; ok {
		set = existingSet
	} else {
		return false
	}

	macroExisted := set.Undefine(macro, replace)
	mc.sets[mc.cur] = set
	return macroExisted
}

// SetCurrentMacroset allows the current macroset to be selected. If it
// doesn't yet exist, it will be created and saved on the first call to Define.
func (mc *MacroCollection) SetCurrentMacroset(setName string) error {
	if mc.sets == nil {
		// the only case where mc.cur will not already exist is the default
		// macroset, sanity check that this has not been violated
		if mc.cur != "" {
			// should never happen
			return fmt.Errorf("sanity check failed: prev call to SetCurrentMacroset did not properly create macroset")
		}

		mc.sets = make(map[string]macroset)
	}

	// if the set does not exist in our table, add it now so we can track the name
	if _, exists := mc.sets[strings.ToUpper(setName)]; !exists {
		mc.sets[strings.ToUpper(setName)] = macroset{
			name:      setName,
			MinLength: mc.MinLength,
		}
	}

	mc.cur = strings.ToUpper(setName)
	return nil
}

// GetCurrentMacroset shows the currently selected Macroset's name.
func (mc *MacroCollection) GetCurrentMacroset() string {
	if mc.sets == nil {
		if mc.cur != "" {
			panic("sanity check failed: prev call to SetCurrentMacroset did not properly create macroset")
		}

		// safe to return "non-user-defined" version for default set because it is always blank.
		return mc.cur
	}

	return mc.sets[mc.cur].GetName()
}

// RenameSet allows a set to be redefined. If the default
// set "" is renamed, it is copied to the new name and a new
// default set is created.
func (mc *MacroCollection) RenameSet(oldName, newName string) error {
	// special case for renaming the default macroset when it is empty
	// (which is allowed)
	if oldName == "" {
		if mc.sets == nil {
			mc.sets = make(map[string]macroset)
		}
		if _, exists := mc.sets[""]; !exists {
			mc.sets[strings.ToUpper(newName)] = macroset{
				MinLength: mc.MinLength,
			}
		}
	}

	old := strings.ToUpper(oldName)
	new := strings.ToUpper(newName)
	set, exists := mc.sets[old]
	if !exists {
		return fmt.Errorf("no macroset named %q exists", oldName)
	}
	set.MinLength = mc.MinLength
	if err := set.SetName(newName); err != nil {
		return err
	}
	mc.sets[new] = set
	delete(mc.sets, old)

	if mc.cur == old {
		mc.cur = new
	}
	return nil
}

// Rename changes the name of a macro in the current macroset. If replace is given,
// also updates all usages of the macro's name in all other macros to match.
func (mc *MacroCollection) Rename(oldName string, newName string, replace bool) error {
	if !mc.IsDefined(oldName) {
		return fmt.Errorf("no macro named %q exists", oldName)
	}
	set := mc.sets[mc.cur]
	if err := set.Rename(oldName, newName, replace); err != nil {
		return err
	}
	mc.sets[mc.cur] = set
	return nil
}

// GetNames gives a list of all macro names in the current set.
func (mc *MacroCollection) GetNames() []string {
	if mc.sets == nil {
		return nil
	}
	if _, ok := mc.sets[mc.cur]; !ok {
		return nil
	}
	return mc.sets[mc.cur].GetAll()
}

// GetNamesIn gives a list of all macro names in the given set.
func (mc *MacroCollection) GetNamesIn(setName string) []string {
	if mc.sets == nil {
		return nil
	}

	if _, ok := mc.sets[strings.ToUpper(setName)]; !ok {
		return nil
	}
	return mc.sets[strings.ToUpper(setName)].GetAll()
}

// GetSetNames gives a list of all defined macroset names, including the current one.
func (mc *MacroCollection) GetSetNames() []string {
	names := []string{}
	addedBlank := false
	if mc.sets == nil {
		return nil
	}
	for k := range mc.sets {
		if k == "" {
			addedBlank = true
		}
		names = append(names, mc.sets[k].GetName())
	}
	if !addedBlank {
		names = append(names, "")
	}

	sort.Strings(names)
	return names
}

// IsDefinedMacroset returns whether the given macroset is defined with items.
func (mc MacroCollection) IsDefinedMacroset(setName string) bool {
	if !mc.macrosetExists(setName) {
		return false
	}
	return mc.sets[strings.ToUpper(setName)].Len() > 0
}

// macrosetExists safely checks if it does without checking
// the items in it.
func (mc MacroCollection) macrosetExists(setName string) bool {
	if mc.sets == nil {
		return false
	}
	_, ok := mc.sets[strings.ToUpper(setName)]
	return ok
}

// ExportSet exports the requested macroset to the given writer.
func (mc *MacroCollection) ExportSet(setName string, w io.Writer) (setsExported int, macrosExported int, err error) {
	if !mc.macrosetExists(setName) {
		if setName == "" {
			// default macroset does not error if it does not exist
		}
		return 0, 0, fmt.Errorf("no macroset named %q currently exists", setName)
	}

	set := mc.sets[strings.ToUpper(setName)]

	bufW := bufio.NewWriter(w)

	if set.Len() > 0 {
		if set.name != "" {
			// write section header
			if _, err := bufW.WriteRune('['); err != nil {
				return 0, 0, fmt.Errorf("while exporting macroset %q: %v", setName, err)
			}
			if _, err := bufW.WriteString(set.name); err != nil {
				return 0, 0, fmt.Errorf("while exporting macroset %q: %v", setName, err)
			}
			if _, err := bufW.WriteString("]\n"); err != nil {
				return 0, 0, fmt.Errorf("while exporting macroset %q: %v", setName, err)
			}
			// we're about to pass control of underlying writer to another routine;
			// flush our buffering first
			if err := bufW.Flush(); err != nil {
				return 0, 0, fmt.Errorf("while exporting macroset %q: %v", setName, err)
			}
		}

		// then write actual macros
		if err := set.Export(w); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", setName, err)
		}
		if _, err := bufW.WriteRune('\n'); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", setName, err)
		}
		if err := bufW.Flush(); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", setName, err)
		}
		return 1, set.Len(), nil
	}
	// nothing in the set, so go ahead and return no export for this one:
	return 0, 0, nil
}

// Export exports all macroset definitions to the given writer.
func (mc *MacroCollection) Export(w io.Writer) (setsExported int, macrosExported int, err error) {
	if mc.sets == nil {
		return 0, 0, nil
	}

	bufW := bufio.NewWriter(w)

	// always do the default one first, and only if it has definitions
	if mc.IsDefinedMacroset("") {
		set := mc.sets[""]
		if err := set.Export(w); err != nil {
			return 0, 0, fmt.Errorf("while exporting the default macroset: %v", err)
		}
		if _, err := bufW.WriteRune('\n'); err != nil {
			return 0, 0, fmt.Errorf("while exporting the default macroset: %v", err)
		}
		if err := bufW.Flush(); err != nil {
			return 0, 0, fmt.Errorf("while exporting the default macroset: %v", err)
		}
		setsExported++
		macrosExported += set.Len()
	}

	// alphabetize them
	setNames := mc.GetSetNames()
	for _, name := range setNames {
		if name == "" {
			// discard the default because it is always in GetSetNames even if not defined
			// and doing it here may impact ordering.
			continue
		}

		// write section header
		if _, err := bufW.WriteRune('['); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", name, err)
		}
		if _, err := bufW.WriteString(name); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", name, err)
		}
		if _, err := bufW.WriteString("]\n"); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", name, err)
		}
		// we're about to pass control of underlying writer to another routine;
		// flush our buffering first
		if err := bufW.Flush(); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", name, err)
		}

		// then write actual macros
		set := mc.sets[strings.ToUpper(name)]
		if err := set.Export(w); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", name, err)
		}
		if _, err := bufW.WriteRune('\n'); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", name, err)
		}
		if err := bufW.Flush(); err != nil {
			return 0, 0, fmt.Errorf("while exporting macroset %q: %v", name, err)
		}
		macrosExported += set.Len()
		setsExported++
	}

	return setsExported, macrosExported, nil
}

// Import reads macroset definitions from the given writer and applies them
// to the current macro collection. They are added rather than removed entirely.
func (mc *MacroCollection) Import(r io.Reader) (setsLoaded int, macrosLoaded int, err error) {
	// so we dont go into a weird state on error, make all changes
	// to a new macrocollection to validate then destroy the extra MacroCollection
	dummy := MacroCollection{MinLength: mc.MinLength}

	scan := bufio.NewScanner(r)
	lineNo := 0
	for scan.Scan() {
		lineNo++
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}

		if setSectionRegex.MatchString(line) {
			secName := strings.Trim(line, "[]")
			if err := dummy.SetCurrentMacroset(secName); err != nil {
				return 0, 0, fmt.Errorf("on line %d: %v", lineNo, err)
			}
		} else {
			// parse as a macro
			macroName, macroContent, err := parseMacroImportLine(line)
			if err != nil {
				return 0, 0, fmt.Errorf("on line %d: %v", lineNo, err)
			}
			if err := dummy.Define(macroName, macroContent); err != nil {
				return 0, 0, fmt.Errorf("on line %d: %v", lineNo, err)
			}
		}
	}
	if err := scan.Err(); err != nil {
		return 0, 0, fmt.Errorf("problem reading input: %v", err)
	}

	// everything is now fully loaded. time to merge the two.
	for _, setName := range dummy.GetSetNames() {
		if dummy.IsDefinedMacroset(setName) {
			dummySet := dummy.sets[strings.ToUpper(setName)]
			for _, macroName := range dummySet.GetAll() {
				macroContent := dummySet.Get(macroName)
				if err := mc.DefineIn(setName, macroName, macroContent); err != nil {
					// should never happen
					return 0, 0, fmt.Errorf("got problem copying from dummy mc to new one: %v", err)
				}
				macrosLoaded++
			}
			setsLoaded++
		}
	}

	return setsLoaded, macrosLoaded, nil
}

// Clear removes all currently-defined macrosets as well as their definitions.
// The current set remains as it was prior to the clear, even if that set is cleared.
func (mc *MacroCollection) Clear() {
	if mc.sets == nil {
		return
	}

	for _, setName := range mc.GetSetNames() {
		if setName != "" || mc.IsDefinedMacroset("") {
			set := mc.sets[strings.ToUpper(setName)]
			set.Clear()
			mc.sets[strings.ToUpper(setName)] = set
			// dont remove the entry if it's the current
			// or if it's the default
			if set.name != "" && strings.ToUpper(set.name) != mc.cur {
				delete(mc.sets, strings.ToUpper(setName))
			}
		}
	}
}
