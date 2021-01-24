package misc

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var spaceCollapser = regexp.MustCompile(" +")

// WrapText wraps the given text to some width. All runs of whitespace are
// normalized to a singlespace before wrapping.
func WrapText(text string, width int) []string {
	// normalize string to convert all whitespace to single space char
	// and ensure there is a period at the end.
	textRunes := []rune(text)
	for i := 0; i < len(textRunes); i++ {
		if unicode.IsSpace(textRunes[i]) {
			textRunes[i] = ' ' // set it to actual space char
		}
	}
	text = string(textRunes)
	text = spaceCollapser.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)
	if !strings.HasSuffix(text, ".") {
		text += "."
	}

	var lines []string
	toConsume := []rune(text)
	curWord := []rune{}
	curLine := []rune{}
	for i := 0; i < len(toConsume); i++ {
		ch := toConsume[i]
		if ch == ' ' {
			lines, curLine = appendWordToLine(lines, curWord, curLine, width)
			curWord = []rune{}
		} else {
			curWord = append(curWord, ch)
		}
	}

	if len(curWord) != 0 {
		lines, curLine = appendWordToLine(lines, curWord, curLine, width)
		curWord = []rune{}
	}

	if len(curLine) != 0 {
		lines = append(lines, string(curLine))
	}
	return lines
}

// Pluralize returns the plural form of the word unless the number is exactly 1, in which case it will return
// the singular form.
func Pluralize(singular string, plural string, count int) string {
	if count == 1 {
		return singular
	}
	return plural
}

// CountOf returns the number followed by either the singular or plural depending on the number.
func CountOf(singular string, plural string, count int) string {
	return fmt.Sprintf("%d %s", count, Pluralize(singular, plural, count))
}

// JustifyTextBlock runs a Justify on every line except for the last.
func JustifyTextBlock(text []string, width int) []string {
	justified := make([]string, len(text))
	for idx, line := range text {
		if idx+1 < len(text) {
			justified[idx] = JustifyText(line, width)
		}
	}
	justified[len(text)-1] = text[len(text)-1]
	return justified
}

// JustifyText takes the given text and attempts to justify it.
// If there are no spaces in the given string, or if it is longer than the
// desired width, the string is returned without being changed.
func JustifyText(text string, width int) string {
	textRunes := []rune(text)
	curLength := len(textRunes)
	if curLength >= width {
		return text
	}

	splitWords := strings.Split(text, " ")
	numGaps := len(splitWords) - 1
	if numGaps < 1 {
		return text
	}
	fullList := []string{}
	for idx, word := range splitWords {
		fullList = append(fullList, word)
		if idx+1 < len(splitWords) {
			fullList = append(fullList, " ")
		}
	}

	spacesToAdd := width - curLength
	spaceIdx := 0
	fromRight := false
	oddSubtractor := 1
	if numGaps%2 == 0 {
		oddSubtractor = 0
	}
	for i := 0; i < spacesToAdd; i++ {
		spaceWordIdx := (spaceIdx * 2) + 1
		if fromRight {
			spaceWordIdx = (((numGaps - oddSubtractor) - spaceIdx) * 2) + 1
		}
		fullList[spaceWordIdx] = fullList[spaceWordIdx] + " "
		fromRight = !fromRight
		spaceIdx++
		if spaceIdx >= numGaps {
			spaceIdx = 0
		}
	}

	finishedWord := strings.Join(fullList, "")
	return finishedWord
}

// CollapseWhitespace converts sequences of any character that is considered
// whitespace by unicode (any rune r for which unicode.IsSpace(r) returns true)
// are converved to a single instance of the space (' ') character.
func CollapseWhitespace(s string) (collapsed string) {
	var sb strings.Builder

	var inSpaceSequence bool
	for _, ch := range s {
		if unicode.IsSpace(ch) {
			inSpaceSequence = true
		} else {
			if inSpaceSequence {
				sb.WriteRune(' ')
				inSpaceSequence = false
			}
			sb.WriteRune(ch)
		}
	}
	if inSpaceSequence {
		sb.WriteRune(' ')
	}
	return sb.String()
}

func appendWordToLine(lines []string, curWord []rune, curLine []rune, width int) (newLines []string, newCurLine []rune) {
	//originalWord := string(curWord)
	for len(curWord) > 0 {
		addedChars := len(curWord)
		if len(curLine) != 0 {
			addedChars++ // for the space
		}
		if len(curLine)+addedChars == width {
			if len(curLine) != 0 {
				curLine = append(curLine, ' ')
			}
			curLine = append(curLine, curWord...)
			lines = append(lines, string(curLine))
			curLine = []rune{}
			curWord = []rune{}
		} else if len(curLine)+addedChars > width {
			if len(curLine) == 0 {
				curLine = append([]rune{}, curWord[0:width-1]...)
				curLine = append(curLine, '-')
				curWord = curWord[width-1:]
			}
			lines = append(lines, string(curLine))
			curLine = []rune{}
		} else {
			if len(curLine) != 0 {
				curLine = append(curLine, ' ')
			}
			curLine = append(curLine, curWord...)
			curWord = []rune{}
		}
	}
	return lines, curLine
}
