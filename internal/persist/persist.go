// Package persist provides a file-like API for all persistence. Everything is represented
// as "documents", each identified by a key (usually a path).
//
// Users of the package create an implementor of Store (usually via NewXStore() function),
// and then can use it to get a ReadWriteCloser for documents that can be read and written
// from. By default, the document is guaranteed to be persisted on a successful close; no
// change is persisted until that point. This can be altered by setting Synchronous on
// the DocumentMode at open time; care must be taken as if this mode is used, the user
// is fully responsible for undoing any writes to the Document that occur prior to any
// operation that causes the Document to fail, should they need to.
//
// OpenDocument, Open, and Create are used to retrieve the Document. Each can retrieve one
// for a specific key in the store as well as an alternate "fully-qualified" key that is
// retrieved instead if it is non-zero. The meaning of "fully-qualified" is different
// based on the implementation of Store; for file-system-based store in a directory, this
// may be a fully-qualified path on the file system as opposed to relative to the initial
// directory; for others, this may be a different meaning.
//
// Concurrent usage is not currently safe.
package persist

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
)

// AllowedOperations is a number that specifies which operations
// are allowed on a Document.
type AllowedOperations int

const (
	// ReadOnly indicates that only reading is allowed on the Document.
	ReadOnly AllowedOperations = iota
	// WriteOnly indicates that only writing is allowed on the Document.
	WriteOnly
	// ReadAndWrite indicates that both writing and reading is allowed on the Document.
	ReadAndWrite
)

// DocumentMode is the mode to open a Document with.
type DocumentMode struct {
	// AllowedOperations is which types of operations are allowed in the document.
	// This can be one of "ReadOnly", "WriteOnly", or "ReadAndWrite".
	AllowedOperations AllowedOperations

	// Append is whether writes append to the end of the Document. If this is set to
	// true, all writes will be sent to the end of the Document regardless of the
	// current seek position.
	Append bool

	// Create specifies that a new Document should be created if one does not already exist.
	Create bool

	// Exclusive is used with Create and specifies that there must not already be a
	// Document that is being overwritten.
	Exclusive   bool
	Synchronous bool
	Truncate    bool
}

// Store for all persistence documents. Each document has a key associated with it,
// usually this is a path or a path-like string referring to the document. Note that
// not all persistence is necessarily file-system based and depends on how the source
// is implemented.
//
type Store interface {
	OpenDocument(key, fqAltKey string, mode DocumentMode)
}

// source for persistence in case we want to change it up later
//
// currently might seen v silly since all we have is "directory, with files",
// but that may change in the future.
//
// (probs yagni but fuck it this is my house and my house shall be tidy)
type dirSource struct {
	dirBased string
}

func (state *consoleState) loadPersistenceFiles() {
	state.loadHistFile()
	state.loadMacrosFile()
	state.loadStateFile()
}

func (state *consoleState) loadMacrosFile() {
	if !state.usingUserPersistenceFiles {
		return
	}
	f, err := openPersistenceFile(state.macrofile, "macros.m")
	if err != nil && !os.IsNotExist(err) {
		state.out.Warn("%v", err)
		state.usingUserPersistenceFiles = false
	}
	defer f.Close()
	state.macros.Clear()
	_, _, err = state.macros.Import(f)
	if err != nil {
		state.out.Warn("couldn't read macros file: %v\n", err)
	}
}

func (state *consoleState) loadStateFile() {
	if !state.usingUserPersistenceFiles {
		return
	}
	f, err := openPersistenceFile("", "state")
	if err != nil {
		state.out.Warn("%v", err)
		state.usingUserPersistenceFiles = false
	}
	defer f.Close()

	dec := gob.NewDecoder(bufio.NewReader(f))
	var curSet string
	if err := dec.Decode(&curSet); err != nil {
		state.out.Warn("couldn't read state file: %v\v", err)
	}
	state.macros.SetCurrentMacroset(curSet)
}

func (state *consoleState) loadHistFile() {
	if !state.usingUserPersistenceFiles {
		return
	}
	f, err := openPersistenceFile("", "history-nkk")
	if err != nil && !os.IsNotExist(err) {
		state.out.Warn("%v", err)
		state.usingUserPersistenceFiles = false
	}
	defer f.Close()
	_, err = state.prompt.ReadHistory(f)
	if err != nil {
		state.out.Warn("couldn't read history file: %v\n", err)
	}
}

func (state *consoleState) writeMacrosFile() {
	if !state.usingUserPersistenceFiles {
		return
	}
	f, err := createPersistenceFile(state.macrofile, "macros.m")
	if err != nil {
		state.out.Warn("%v", err)
		state.usingUserPersistenceFiles = false
	}
	defer f.Close()
	_, _, err = state.macros.Export(f)
	if err != nil {
		state.out.Warn("couldn't write macros file: %v\n", err)
		state.usingUserPersistenceFiles = false
	}
}

func (state *consoleState) writeHistFile() {
	if !state.usingUserPersistenceFiles {
		return
	}
	f, err := createPersistenceFile("", "history-nkk")
	if err != nil {
		state.out.Warn("%v", err)
		state.usingUserPersistenceFiles = false
	}
	defer f.Close()
	_, err = state.prompt.WriteHistory(f)
	if err != nil {
		state.out.Warn("couldn't write history file: %v\n", err)
		state.usingUserPersistenceFiles = false
	}
}

func (state *consoleState) writeStateFile() {
	if !state.usingUserPersistenceFiles {
		return
	}
	f, err := createPersistenceFile("", "state")
	if err != nil {
		state.out.Warn("%v", err)
		state.usingUserPersistenceFiles = false
	}
	defer f.Close()

	enc := gob.NewEncoder(bufio.NewWriter(f))
	if err := enc.Encode(state.macros.GetCurrentMacroset()); err != nil {
		state.out.Warn("couldn't write state file: %v\v", err)
	}
}

func createPersistenceFile(userSupplied, defaultIfNone string) (*os.File, error) {
	fullPath, err := getPersistencePath(userSupplied, defaultIfNone)
	if err != nil {
		return nil, err
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't open ~/.netkk/%s; persistence will be limited to this session: %v", filepath.Base(fullPath), err)
	}
	return f, nil
}

func openPersistenceFile(userSupplied, defaultIfNone string) (*os.File, error) {
	fullPath, err := getPersistencePath(userSupplied, defaultIfNone)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't open ~/.netkk/%s; persistence will be limited to this session: %v", filepath.Base(fullPath), err)
	}
	return f, nil
}

func getPersistencePath(userSupplied, defaultIfNone string) (string, error) {
	var fullPath string
	if userSupplied != "" {
		fullPath = userSupplied
	} else {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("couldn't get homedir; persistence will be limited to this session: %v", err)
		}
		appDir := filepath.Join(homedir, ".netkk")
		err = os.Mkdir(appDir, os.ModeDir|0755)
		if err != nil && !os.IsExist(err) {
			return "", fmt.Errorf("couldn't create ~/.netkk; persistence will be limited to this session: %v", err)
		}
		fullPath = filepath.Join(appDir, defaultIfNone)
	}
	return fullPath, nil
}
