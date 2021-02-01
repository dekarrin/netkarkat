package console

import (
	"dekarrin/netkarkat/internal/misc"
	"fmt"
	"strings"
	"unicode/utf8"
)

func showHelp(topic string) string {
	var sb strings.Builder
	helpWidth := 80
	nameSuffix := "- "

	if topic != "" {
		cmd, ok := commands[strings.ToUpper(topic)]
		if !ok {
			sb.WriteString(fmt.Sprintf("Unknown command %q; try just HELP for a list of commands", topic))
		} else {
			if cmd.aliasFor != "" {
				cmd = commands[cmd.aliasFor]
			}

			topic = strings.ToUpper(topic)

			leftColumnWidth := utf8.RuneCountInString(buildHelpCommandName(topic))
			leftColumnWidth += utf8.RuneCountInString(nameSuffix)
			descWidth := helpWidth - leftColumnWidth
			for descWidth < 2 {
				helpWidth++
				descWidth = helpWidth - leftColumnWidth
			}
			writeHelpForCommand(topic, &sb, descWidth, leftColumnWidth, nameSuffix)
		}
	} else {

		// first build the initial
		leftColumnWidth := -1
		for name, c := range commands {
			if c.aliasFor != "" {
				continue
			}
			colName := buildHelpCommandName(name)
			if utf8.RuneCountInString(colName) > leftColumnWidth {
				leftColumnWidth = utf8.RuneCountInString(colName)
			}
		}
		leftColumnWidth += utf8.RuneCountInString(nameSuffix)
		descWidth := helpWidth - leftColumnWidth
		for descWidth < 2 {
			helpWidth++
			descWidth = helpWidth - leftColumnWidth
		}
		sb.WriteString("Commands:\n")
		for _, name := range commands.names() {
			if commands[name].aliasFor != "" {
				continue
			}
			if name == "HELP" || name == "EXIT" { // special cases; these come at the end
				continue
			}
			writeHelpForCommand(name, &sb, descWidth, leftColumnWidth, nameSuffix)
		}

		writeHelpForCommand("HELP", &sb, descWidth, leftColumnWidth, nameSuffix)
		writeHelpForCommand("EXIT", &sb, descWidth, leftColumnWidth, nameSuffix)

		suffix := `By default, input will be read until a newline is encountered. To change this behavior,
		use the '--multiline' flag at launch to read until a semi-colon character is encountered.

		Any input that does not match one of the built-in commands is sent to the
		remote server.

		If input must be sent that includes one of the built-in commands at the start,
		the SEND command can be used to avoid pattern matching everything after it.`

		suffixLines := misc.WrapText(suffix, helpWidth)
		suffixLines = misc.JustifyTextBlock(suffixLines, helpWidth)
		for _, line := range suffixLines {
			sb.WriteString(line)
			sb.WriteRune('\n')
		}
	}
	return sb.String()
}

func buildHelpCommandName(name string) string {
	c := commands[name]
	allNames := strings.Join(commands.getAllAliasesOf(name), "/")
	colName := allNames + " "
	if c.helpInvoke != "" {
		colName += c.helpInvoke + " "
	}
	colName += " "
	return colName
}

func writeHelpForCommand(name string, sb *strings.Builder, descWidth int, leftColumnWidth int, nameSuffix string) {
	helpDescLines := misc.WrapText(commands[name].helpDesc, descWidth)
	helpDescLines = misc.JustifyTextBlock(helpDescLines, descWidth)

	cmdName := buildHelpCommandName(name)
	sb.WriteString(cmdName)
	for i := 0; i < leftColumnWidth-(utf8.RuneCountInString(cmdName)+utf8.RuneCountInString(nameSuffix)); i++ {
		sb.WriteRune(' ')
	}
	sb.WriteString(nameSuffix)
	sb.WriteString(helpDescLines[0])
	sb.WriteRune('\n')
	for i := 1; i < len(helpDescLines); i++ {
		for j := 0; j < leftColumnWidth; j++ {
			sb.WriteRune(' ')
		}
		sb.WriteString(helpDescLines[i])
		sb.WriteRune('\n')
	}
	sb.WriteRune('\n')
}
