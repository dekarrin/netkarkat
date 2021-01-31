package stack

// StringStack is a stack of strings. The zero value is safe to use.
type StringStack struct {

	// Normalize is a function that strings are passed through before being added to the
	// stack or compared to items in the stack. Passing in a function here changes every
	// string passed in; if for instance, strings.ToUpper is passed in, all strings paased
	// to the StringStack for comparison or addition to the stack are transformed to upper
	// case, making all comparisons case insensitive.
	//
	// If no function is set, it will simply not be called.
	Normalize func(string) string

	// DenormalizeOnExit is whether to return the original string as it was inserted on
	// exit from the stack when a normalize function placed it there originally.
	// The default is false, where returned strings are returned in their normalized
	// form
	DenormalizeOnExit bool
	existence         map[string]int
	order             []string
	original          []string
}

// Contains returns whether the given string is in the stack.
func (sstack StringStack) Contains(s string) bool {
	_, in := sstack.existence[sstack.normalizeIfDefined(s)]
	return in
}

// Len return the current number of items.
func (sstack StringStack) Len() int {
	return len(sstack.order)
}

// Push pushes a new item on to the top of the stack.
func (sstack *StringStack) Push(s string) {
	if sstack.order == nil {
		sstack.order = make([]string, 0)
		sstack.original = make([]string, 0)
		sstack.existence = make(map[string]int, 0)
	}
	norm := sstack.normalizeIfDefined(s)
	sstack.order = append(sstack.order, norm)
	sstack.original = append(sstack.original, s)
	if _, alreadyPresent := sstack.existence[norm]; !alreadyPresent {
		sstack.existence[norm] = 0
	}
	sstack.existence[norm]++
}

// PushFront pushes a new item on to the bottom of the stack.
func (sstack *StringStack) PushFront(s string) {
	if sstack.order == nil {
		sstack.order = make([]string, 0)
		sstack.original = make([]string, 0)
		sstack.existence = make(map[string]int, 0)
	}
	norm := sstack.normalizeIfDefined(s)
	sstack.order = append([]string{norm}, sstack.order...)
	sstack.original = append([]string{s}, sstack.original...)
	if _, alreadyPresent := sstack.existence[norm]; !alreadyPresent {
		sstack.existence[norm] = 0
	}
	sstack.existence[norm]++
}

// Pop removes and returns the item currently on the top of the stack.
// If the length is zero, panics.
func (sstack *StringStack) Pop() string {
	if sstack.Len() < 1 {
		panic("tried to pop from an empty stack")
	}

	norm, s := sstack.getAt(sstack.Len() - 1)
	sstack.deleteIndex(sstack.Len() - 1)

	sstack.existence[norm]--
	if sstack.existence[norm] < 1 {
		delete(sstack.existence, norm)
	}
	return s
}

// PopIfOk removes and returns the item currently on the top of the stack.
// If the length is zero, ok will be false and the string should not be
// used.
func (sstack *StringStack) PopIfOk() (s string, ok bool) {
	if sstack.Len() < 1 {
		return "", false
	}
	return sstack.Pop(), true
}

// PopFront removes and returns the item currently on the bottom of the stack.
// If the length is zero, panics.
func (sstack *StringStack) PopFront() string {
	if sstack.Len() < 1 {
		panic("tried to pop from an empty stack")
	}

	norm, s := sstack.getAt(0)
	sstack.deleteIndex(0)

	sstack.existence[norm]--
	if sstack.existence[norm] < 1 {
		delete(sstack.existence, norm)
	}
	return s
}

// PopFrontIfOk removes and returns the item currently on the bottom of the stack.
// If the length is zero, ok will be false and the string should not be
// used.
func (sstack *StringStack) PopFrontIfOk() (s string, ok bool) {
	if sstack.Len() < 1 {
		return "", false
	}
	return sstack.PopFront(), true
}

// Peek rereturns the item currently on the top of the stack.
// If the length is zero, panics.
func (sstack *StringStack) Peek() string {
	if sstack.Len() < 1 {
		panic("tried to peek from an empty stack")
	}

	_, s := sstack.getAt(sstack.Len() - 1)
	return s
}

// PeekIfOk rereturns the item currently on the top of the stack.
// If the length is zero, ok will be false and the string should not be
// used.
func (sstack *StringStack) PeekIfOk() (s string, ok bool) {
	if sstack.Len() < 1 {
		return "", false
	}
	return sstack.Peek(), true
}

// PeekFront rereturns the item currently on the bottom of the stack.
// If the length is zero, panics.
func (sstack *StringStack) PeekFront() string {
	if sstack.Len() < 1 {
		panic("tried to peek from an empty stack")
	}

	_, s := sstack.getAt(0)
	return s
}

// PeekFrontIfOk rereturns the item currently on the bottom of the stack.
// If the length is zero, ok will be false and the string should not be
// used.
func (sstack *StringStack) PeekFrontIfOk() (s string, ok bool) {
	if sstack.Len() < 1 {
		return "", false
	}
	return sstack.PeekFront(), true
}

func (sstack StringStack) normalizeIfDefined(s string) string {
	if sstack.Normalize != nil {
		return sstack.Normalize(s)
	}
	return s
}

func (sstack StringStack) getDenormIfDefined(idx int) string {
	if sstack.Normalize == nil || sstack.DenormalizeOnExit {
		return sstack.order[idx]
	}
	return sstack.original[idx]
}

func (sstack StringStack) getAt(idx int) (norm string, orig string) {
	return sstack.order[idx], sstack.getDenormIfDefined(idx)
}

func (sstack *StringStack) deleteIndex(idx int) {
	sstack.order = append(sstack.order[:idx], sstack.order[idx+1:]...)
	sstack.original = append(sstack.original[:idx], sstack.original[idx+1:]...)
}
