/*
Package verbosity handles suppression and allowing of output based on a configured "verboseness"
for a program.

Verbosity And Level Types

The Verbosity and Level types are the core of the suppression system. Together,
they determine whether something should be printed to the screen or not.

Values for Verbosity are static and defined by this package, but new Levels
can be defined at any point as needed by users of this package.

The Verbosity values provided each offer a different level of verboseness to a
program. In order of number of Levels suppressed, they are: Silent, Quiet,
Normal, Verbose, SuperVerbose, and FullyVerbose. Two of these values are
special; FullyVerbose is the zero-value for a Verbosity and allows all levels,
and Silent allows no Levels at all.

The Levels give a priority for an action to be taken (usually printing output).
The following default Levels are provided, in order of their priority:
Trace, Debug, Info, Warn, Critical, Error. Warn and Error have priorities
that match Info and Critical respectively; note that this means that any
Verbosity that suppresses Info will also suppress Warn, and any Verbosity that
suppress Critical will also suppress Error.

Checking If Action Should Be Taken

To determine if an output-producing action should be taken based on a Verbosity,
a Level for that action must be determined by the caller, and then the Verbosity
can be queried as to whether that Level of action should be allowed with the
Allows() function:

	Normal.Allows(Info)    // returns true
	Silent.Allows(Info)    // returns false
	Verbose.Allows(Debug)  // returns true

	func PrintIfAllowed(verb Verbosity, level Level, message string) {
		if verb.Allows(level) {
			fmt.Println(message)
		}
	}

	PrintIfAllowed(Verbose, Debug, "DEBUG: this is a debug message")  // This will be printed

	PrintIfAllowed(Quiet,   Debug, "DEBUG: this is a debug message")  // This will not be printed

Parsing Verbosity from CLI

The amount of verboseness that a program has is typically set via CLI flags;
there is usually one or more 'verbose' (or '-v') options that can be passed in,
and usually a single flag for 'quiet mode'.

To simplify parsing this to a Verbosity, the ParseFromFlags() function can be
used.

	numTimesVerboseWasSet := 2
	quietWasSet := false

	verb := ParseFromFlags(quietWasSet, numTimesVerboseWasSet)

Levels

Levels are used to give the priority of an action. Every Level has two
properties: a priority, and a name. The priority is used to determine whether a
Verbosity will allow a Level, and the name can be used to output the level when
printing the message:

	func Output(verb Verbosity, level Level, message string) {
		if verb.Allows(level) {
			fmt.Printf("%s: %s\n", level.Name(), message)
		}
	}

	Output(Verbose, Debug, "a debug message")  // Will print "DEBUG: a debug message"

	Output(Verbose, Trace, "a trace message")  // Will not print anything

If the user of this package needs more extensive priority systems than the
provided ones provide, or if the user needs a custom name, a custom level may
be created. This can be done with the NewLevel() function.

Generally, a custom level should be given a priority relative to an existing
level; the first pre-defined Level, Trace, is gauranteed to have a priority of
PrioritySeparation, and every subsequently higher-priority level will have a
priority that is higher by an additioanl PrioritySepearation. The minimum that
PrioritySeparation is gauranteed to be is 10.

	func Output(verb Verbosity, level Level, message string) {
		if verb.Allows(level) {
			fmt.Printf("%s: %s\n", level.Name(), message)
		}
	}

	importantLevel := NewLevel(Info.Priority() + 1, "IMPORTANT")

	Output(Trace, importantLevel, "started")  // Will print: "IMPORTANT: started"

OutputWriter

In order to make the use of this package more convenient, functionality related
to checking a verbosity, printing a message if at the correct level, and logging
is encapsulated by the OutputWriter object. This can be easily and quickly used
to define output policy for an application and pass it between functions:

	var out OutputWriter
	out.Verbosity = Normal

	out.Info("this will be printed")
	out.Warn("this will also be printed")

	// The OutputWriter's Verbosity of Normal does not allow the Debug level, so
	// this will not be printed:
	out.Debug("this will not be printed")

Logging can also be enabled on an OutputWriter, and if it is, all messages will
be logged, even those that are suppressed by the verbosity. To enable logging,
use the StartLogging() function. Logging can later be disabled with a call to
StopLogging().

	logFile, err := os.Create("logfile.log")
	if err != nil {
		panic("could not open log file for writing")
	}
	defer logFile.Close()

	var out OutputWriter
	out.Verbosity = Normal
	out.StartLogging(logFile)

	out.Info("this will be printed, and logged to logfile.log")
	out.Warn("this will also be printed and logged to logfile.log")

	out.Debug("this will not be printed, but it is still logged to logfile.log")

	out.StopLogging()

	out.Debug("this will not be printed or logged")

It is possible to combine the logging behavior with the Silent Verbosity to make
calls to an OutputWriter behave like calls to log.Printf():

	var out OutputWriter

	// setting to Silent disables all typical output:
	out.Verbosity = Silent
	out.StartLogging(os.Stderr)

	out.Info("this will be logged to stderr")
	out.Warn("this will be logged to stderr")
	out.Debug("this will be logged to stderr")
*/
package verbosity

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"text/template"
	"unicode/utf8"
)

// OutputMessage is the struct that is passed to formatting templates before
// output occurs. Custom templates in an OutputWriter can refer to any field in
// this struct.
type OutputMessage struct {
	Level   Level
	Message string
}

// PrioritySeparation is the amount that each predefined Level's priority is
// separated by. The exceptions to this are Error, which has the same priority
// as Critical, and Warn, which has the same priority as Info.
const PrioritySeparation = 100

// DefaultStdoutTemplateStr defines the default template that will be used for
// outputting messages to Stdout. It is used by OutputWriter if its
// StdoutTemplate is set to a zero-value.
const DefaultStdoutTemplateStr = "{{.Message}}"

var defaultStdoutTemplate = template.Must(template.New("default-stdout").Parse(DefaultStdoutTemplateStr))

// DefaultStderrTemplateStr defines the default template that will be used for
// outputting messages to Stderr. It is used by OutputWriter if its
// StderrTemplate is set to a zero-value.
const DefaultStderrTemplateStr = "{{.Level.Name}}: {{.Message}}"

var defaultStderrTemplate = template.Must(template.New("default-stderr").Parse(DefaultStderrTemplateStr))

// DefaultLogTemplateStr defines the default template that will be used for
// outputting messages to the log. It is used by OutputWriter if its
// LogTemplate is set to a zero-value.
const DefaultLogTemplateStr = "{{.Level.Name}}: {{.Message}}"

var defaultLogTemplate = template.Must(template.New("default-log").Parse(DefaultLogTemplateStr))

// DefaultStderrFilter is the default function for checking if a
// message should be printed to stderr. It is used by VerboseAwareOutputWriter
// if its StderrFilter is set to a zero-value.
//
// This function returns true if the level is exactly equal to Warn, or if the
// level's integer value is greater than or equal to Critical.
func DefaultStderrFilter(lv Level) bool {
	return lv == Warn || lv.Priority() >= Critical.Priority()
}

// Verbosity determines what the current level is and allows writing to/from logs and output streams.
//
// The zero-value is FullyVerbose, which allows all messages.
type Verbosity int

const (
	// Silent will not output any messages regardless of their level. Usually used in conjunction
	// with logging to make an OutputWriter only log messages.
	Silent Verbosity = -1

	// FullyVerbose will output all messages, even those marked as lower-level than trace.
	FullyVerbose Verbosity = 0

	// Quiet will only output messages that are Critical level or higher.
	Quiet Verbosity = 1

	// Normal will only output messages that are Info level or higher.
	Normal Verbosity = 2

	// Verbose will only output messages that are Debug level or higher.
	Verbose Verbosity = 3

	// SuperVerbose will output all messages that are Trace level or higher.
	SuperVerbose Verbosity = 4
)

// ParseFromFlags parses a verbosity by analyzing the number of times that a verbose flag was
// passed in and seeing if quiet mode was enabled.
//
// If quiet mode is enabled, it overrides all verbose flags, and the returned Verbosity will be Quiet.
// If quiet mode is not enabled, the number of times that the verbose mode was set is used to determine
// the result: If it's 0, Normal is returned; if it's 1, Verbose is returned; if it's 2, SuperVerbose
// is returned, and any other amount will return FullyVerbose, which does not suppress any messages.
func ParseFromFlags(quiet bool, verboseNum int) Verbosity {
	if quiet {
		return Quiet
	}
	if verboseNum == 0 {
		return Normal
	} else if verboseNum == 1 {
		return Verbose
	} else if verboseNum == 2 {
		return SuperVerbose
	}
	return FullyVerbose
}

// Allows returns whether the verbosity would allow the given level to
// be passed through as output. It can be used when the normal output
// functions are unsuitable, such as when returning a string whose
// output changes based on the verbosity.
func (ver Verbosity) Allows(level Level) bool {
	switch ver {
	case Silent:
		return false
	case Quiet:
		return level.priority >= Critical.priority
	case Normal:
		return level.priority >= Info.priority
	case Verbose:
		return level.priority >= Debug.priority
	case SuperVerbose:
		return level.priority >= Trace.priority
	case FullyVerbose:
		return true
	default:
		// we have no idea about this verbosenes, and the user shouldn't have created one.
		// we could just say no suppression is applied, but we'll instead just let it act
		// as the highest verbosity, which would be the default behavior if they weren't
		// going through the verbosity library (which, if they have a custom Verbosity,
		// they are not)
		return FullyVerbose.Allows(level)
	}
}

// Level specifies the level at which to print a message at. It is compared to the verbosity to determine whether it
// should be outputted.
//
// There are four pre-defined Levels, each with a particular int value. They start at Trace, the lowest level, and
// moving up from there include Debug, Info, and Critical. Each predefined Level has an int value that differs by at
// least 10, giving plenty of room between to define custom Levels. The lowest level, Trace, starts with a value of
// that difference (so there is room below it for custom levels) and each Level up from there has an int value that
// is a certain number of units (at least 10) greater than the level below it.
//
// The IntValue() method of Level can be used to get the int value of a Level, for use in math while creating your
// own Level with the CustomLevel() function, so you can have one 'just above' or 'just below' an existing Level.
//
// Additionally, there is the "Warn" and "Error" levels; Warn has the same int value as Info but its name in log
// output is "WARN" as opposed to "INFO", and Error has the same int value as Critical but its name in log output
// is "ERROR" as opposed to "CRITICAL"
type Level struct {
	priority int
	name     string
}

// Priority returns the integer value of a level, which can be used to create custom levels that are 'just above'
// or 'just below' other levels. All lev
func (lv Level) Priority() int {
	return lv.priority
}

// Name returns the name of a level
func (lv Level) Name() string {
	return lv.name
}

const (
	tracePriority = PrioritySeparation
	traceName     = "TRACE"

	debugPriority = tracePriority + PrioritySeparation
	debugName     = "DEBUG"

	infoPriority = debugPriority + PrioritySeparation
	infoName     = "INFO"
	warnPriority = infoPriority
	warnName     = "WARN"

	criticalPriority = infoPriority + PrioritySeparation
	criticalName     = "CRITICAL"
	errorPriority    = criticalPriority
	errorName        = "ERROR"
)

var (
	// Critical is an urgent level for a message that will never be silenced.
	Critical = Level{priority: criticalPriority, name: criticalName}

	// Error is the same level for a message as Critical but shows up in output as "ERROR" as opposed to "CRITICAL".
	Error = Level{priority: errorPriority, name: errorName}

	// Info is a typical level for a message that will be silenced by verbosities of Quiet.
	Info = Level{priority: infoPriority, name: infoName}

	// Warn is the same level for a message as Info but shows up in output as "WARN" as opposed to "INFO".
	Warn = Level{priority: warnPriority, name: warnName}

	// Debug is a lower-level of a message that will be silenced by verbosities of Quiet and Normal.
	Debug = Level{priority: debugPriority, name: debugName}

	// Trace is a very low-level of a message that will be silenced by verbosities of Quiet, Normal, and Verbose.
	Trace = Level{priority: tracePriority, name: traceName}
)

// NewLevel is used to convert an integer value to a custom Level object for use in the output functions.
//
// The name is what string is shown as the level when the Level is used in Log() statements.
// If name is set to the empty string, the actual name will be "LEVEL X" where "X" is replaced with
// the string representation of intValue.
func NewLevel(priority int, name string) Level {
	if name == "" {
		name = fmt.Sprintf("%d", priority)
	}
	return Level{priority: priority, name: name}
}

// OutputWriter outputs text based on how important that text is and based on how verbose
// the OutputWriter is set to be. For instance, an INFO-level message is printed if the
// verbosity is not set to quiet.
//
// OutputWriter uses system primitives that should generally not be copied;
//
// All output goes to either Stderr or Stdout (except for Sprintf functions), but
// the threshold at which this happens can be configured.
type OutputWriter struct {

	// Verbosity is the amount of verboseness that is used to determine what levels to allow through.
	Verbosity Verbosity

	// OutputToStderr is a function that returns whether the given level should be outputted
	// to Stderr. If it returns, it will go to Stdout instead.
	//
	// If set to its zero-value, it will be treated as though it were the DefaultStderrFilter
	// function.
	StderrFilter func(Level) bool

	// StderrTemplate is a template that says what the format is to be when writing
	// a message to stderr. The template will be executed on instances of OutputMessage.
	//
	// If set to its zero-value, it will be assumed to the template defined by
	// DefaultStderrTemplateStr.
	//
	// This is ignored by Sprintf functions, which output only "" or the formatted message.
	StderrTemplate *template.Template

	// StdoutTemplate is a template that says what the format is to be when writing
	// a message to stdout. The template will be executed on instances of OutputMessage.
	//
	// If set to its zero-value, it will be assumed to the template defined by
	// DefaultStdoutTemplateStr.
	//
	// This is ignored by Sprintf functions, which output only "" or the formatted message.
	StdoutTemplate *template.Template

	// LotTemplate is a template that says what the format is to be when writing
	// a message to stdout. The template will be executed on instances of OutputMessage.
	//
	// If set to its zero-value, it will be assumed to the template defined by
	// DefaultLogTemplateStr.
	//
	// This is not used unless logging has been enabled on this OutputWriter.
	LogTemplate *template.Template

	// AutoNewline specifies whether newlines should be added to the output if it doesn't
	// already contain one.
	//
	// This is ignored by Sprintf functions and log functions; Sprintf never adds
	// a newline and log functions always do.
	AutoNewline bool

	// AutoCapitalize specifies whether the first letter of each output string
	// should be capitalized.
	//
	// This is ignored by Sprintf functions.
	AutoCapitalize bool

	// note: someone could be asynchronously creating this, so when it is read
	// in a pointer-receiver func, it should always be copied and the copy read.
	logger *log.Logger
}

// StartLogging turns on logging to the given writer for messages sent to the OutputWriter.
// When logging is enabled, all messages are logged regardless of whether they are suppressed
// by the verbosity.
//
// If logging has already started via a previous call to StartLogging(), the old logging
// is replaced by the new one.
func (ow *OutputWriter) StartLogging(writer io.Writer) {
	ow.logger = log.New(writer, "", log.LstdFlags)
}

// StopLogging stops all logging activity.
func (ow *OutputWriter) StopLogging() {
	ow.logger = nil
}

// Log writes a message to the log if logging is enabled. Typical output
// functionality is skipped; if logging is not enabled, calling this function
// will result in no output at all.
func (ow OutputWriter) Log(lv Level, format string, a ...interface{}) {
	if ow.logger == nil {
		return
	}
	t := ow.LogTemplate
	if t == nil {
		t = defaultLogTemplate
	}
	loggedMessage := ow.formatForOutput(t, lv, format, a...)
	ow.logger.Print(loggedMessage)
}

// Output outputs a message if the verbosity for the OutputWriter allows the
// given Level. Regardless of whether it is allowed, the message will be
// logged.
func (ow OutputWriter) Output(lv Level, format string, a ...interface{}) {
	ow.Log(lv, format, a...)
	if ow.Verbosity.Allows(lv) {
		// find out if we are going to stderr or not:
		stderrFunc := ow.StderrFilter
		if stderrFunc == nil {
			stderrFunc = DefaultStderrFilter
		}

		var destStream *os.File
		var t *template.Template
		if stderrFunc(lv) {
			destStream = os.Stderr
			t = ow.StderrTemplate
			if t == nil {
				t = defaultStderrTemplate
			}
		} else {
			destStream = os.Stdout
			t = ow.StdoutTemplate
			if t == nil {
				t = defaultStdoutTemplate
			}
		}

		message := ow.formatForOutput(t, lv, format, a...)

		fmt.Fprint(destStream, message)
	}
}

// Critical outputs the given message at Critical level.
//
// This is equivalent to a call to Output(Critical, format, a...).
func (ow OutputWriter) Critical(format string, a ...interface{}) {
	ow.Output(Critical, format, a...)
}

// Error outputs the given message at Error level.
//
// This is equivalent to a call to Output(Error, format, a...).
func (ow OutputWriter) Error(format string, a ...interface{}) {
	ow.Output(Error, format, a...)
}

// Info outputs the given message at Info level.
//
// This is equivalent to a call to Output(Info, format, a...).
func (ow OutputWriter) Info(format string, a ...interface{}) {
	ow.Output(Info, format, a...)
}

// Warn outputs the given message at Warn level.
//
// This is equivalent to a call to Output(Warn, format, a...).
func (ow OutputWriter) Warn(format string, a ...interface{}) {
	ow.Output(Warn, format, a...)
}

// Debug outputs the given message at Debug level.
//
// This is equivalent to a call to Output(Debug, format, a...).
func (ow OutputWriter) Debug(format string, a ...interface{}) {
	ow.Output(Debug, format, a...)
}

// Trace outputs the given message at Trace level.
//
// This is equivalent to a call to Output(Trace, format, a...).
func (ow OutputWriter) Trace(format string, a ...interface{}) {
	ow.Output(Trace, format, a...)
}

// Sprintf returns a formatted string if the verbosity for the OutputWriter allows the
// given Level; otherwise, it returns an empty string.
//
// Calling this function does not cause logging to occur.
func (ow OutputWriter) Sprintf(lv Level, format string, a ...interface{}) string {
	if ow.Verbosity.Allows(lv) {
		return fmt.Sprintf(format, a...)
	}
	return ""
}

// CriticalSprintf returns a formatted string if the verbosity for the OutputWriter allows
// Critical messages; otherwise, it returns an empty string.
//
// This is equivalent to a call to Sprintf(Critical, format, a...).
func (ow OutputWriter) CriticalSprintf(format string, a ...interface{}) string {
	return ow.Sprintf(Critical, format, a...)
}

// ErrorSprintf returns a formatted string if the verbosity for the OutputWriter allows
// Error messages; otherwise, it returns an empty string.
//
// This is equivalent to a call to Sprintf(Error, format, a...).
func (ow OutputWriter) ErrorSprintf(format string, a ...interface{}) string {
	return ow.Sprintf(Error, format, a...)
}

// InfoSprintf returns a formatted string if the verbosity for the OutputWriter allows
// Info messages; otherwise, it returns an empty string.
//
// This is equivalent to a call to Sprintf(Info, format, a...).
func (ow OutputWriter) InfoSprintf(format string, a ...interface{}) string {
	return ow.Sprintf(Info, format, a...)
}

// WarnSprintf returns a formatted string if the verbosity for the OutputWriter allows
// Warn messages; otherwise, it returns an empty string.
//
// This is equivalent to a call to Sprintf(Warn, format, a...).
func (ow OutputWriter) WarnSprintf(format string, a ...interface{}) string {
	return ow.Sprintf(Warn, format, a...)
}

// DebugSprintf returns a formatted string if the verbosity for the OutputWriter allows
// Debug messages; otherwise, it returns an empty string.
//
// This is equivalent to a call to Sprintf(Debug, format, a...).
func (ow OutputWriter) DebugSprintf(format string, a ...interface{}) string {
	return ow.Sprintf(Debug, format, a...)
}

// TraceSprintf returns a formatted string if the verbosity for the OutputWriter allows
// Trace messages; otherwise, it returns an empty string.
//
// This is equivalent to a call to Sprintf(Trace, format, a...).
func (ow OutputWriter) TraceSprintf(format string, a ...interface{}) string {
	return ow.Sprintf(Trace, format, a...)
}

func (ow OutputWriter) formatForOutput(template *template.Template, lv Level, messageFormat string, messageArgs ...interface{}) string {
	formattedMessage := fmt.Sprintf(messageFormat, messageArgs...)
	om := OutputMessage{Level: lv, Message: formattedMessage}
	buf := bytes.NewBuffer([]byte{})
	template.Execute(buf, om)
	str := string(buf.Bytes())
	if ow.AutoNewline && !strings.HasSuffix(str, "\n") {
		str += "\n"
	}
	if ow.AutoCapitalize {
		str = firstCharToUpper(str)
	}
	return str
}

func firstCharToUpper(str string) string {
	if len(str) < 1 {
		return str
	}
	if utf8.RuneCountInString(str) == 1 {
		str = strings.ToUpper(str)
	} else {
		// it is at least two characters, so:

		seenFirstChar := false
		secondCharIdx := -1

		// this bizarre syntax is because there is no other easier
		// way to get an indexed character from a string as of this
		// writing
		for idx := range str {
			if !seenFirstChar {
				seenFirstChar = true
			} else {
				secondCharIdx = idx
				break
			}
		}

		str = strings.ToUpper(str[0:secondCharIdx]) + str[secondCharIdx:]
	}
	return str
}
