package argvtmpl

import (
	"path/filepath"
	"strconv"
	"strings"
)

// DashError reports an argv element whose first character comes from an
// input value and is "-", so the program would read it as an option.
type DashError struct {
	Input   Input
	Value   string // the value of Input, not the whole argv element
	Program string // base name of argv[0]
}

func (e *DashError) Error() string {
	name := e.Input.Name
	shown := "<" + name + ">"
	if e.Input.Flag {
		shown = "--" + strings.ReplaceAll(name, "_", "-")
	}
	msg := "value " + strconv.Quote(e.Value) + " for " + shown + ` starts with "-" and would be read as an option by ` + e.Program +
		"; use --opt={{." + name + "}}, put a literal -- before it, or set allow_dash"
	if e.Program == "git" {
		msg += ", or --end-of-options"
	}
	return msg
}

// CheckDash returns a *DashError for the first element whose first character
// comes from a value of an input that is neither allow_dash nor rest, is not
// exempted by a literal --, and starts with "-". program is
// filepath.Base(argv[0]).
func CheckDash(args []Arg, inputs []Input, program string) error {
	for _, a := range args {
		if a.First == "" || a.Exempt || !strings.HasPrefix(a.firstValue, "-") {
			continue
		}
		for _, in := range inputs {
			if in.Name == a.First && !in.AllowDash && !in.Rest {
				return &DashError{Input: in, Value: a.firstValue, Program: program}
			}
		}
	}
	return nil
}

// BatchError reports an input value with a character that cmd.exe would
// interpret when running a .bat/.cmd file.
type BatchError struct {
	Input string // the first input with characters in the element
	Char  byte
	File  string // base name of the batch file
}

func (e *BatchError) Error() string {
	return "value for <" + e.Input + "> contains " + strconv.Quote(string(e.Char)) + ", unsafe for batch file " + e.File
}

// batchUnsafe are the characters cmd.exe interprets in batch file arguments.
const batchUnsafe = "\"%!&|<>^\r\n"

// CheckBatch returns a *BatchError when exe (the path after the PATH lookup)
// is a .bat or .cmd file and an element containing an input value contains a
// character unsafe for cmd.exe. It does not check the OS: the caller does.
func CheckBatch(exe string, args []Arg) error {
	if ext := strings.ToLower(filepath.Ext(exe)); ext != ".bat" && ext != ".cmd" {
		return nil
	}
	for _, a := range args {
		if len(a.Users) == 0 {
			continue
		}
		if i := strings.IndexAny(a.Value, batchUnsafe); i >= 0 {
			return &BatchError{Input: a.Users[0], Char: a.Value[i], File: filepath.Base(exe)}
		}
	}
	return nil
}
