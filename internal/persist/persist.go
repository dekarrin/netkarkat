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
// All IO itself is buffered unless Synchronous is passed to the OpenDocument function;
// in that case all IO will be directly to the file. This is likely to be extremely slow.
//
// Concurrent usage is not currently safe.
//
//
//
// currently package might seen v silly since all we have is "directory, with files",
// but that may change in the future.
//
// (probs yagni but fuck it this is my house and my house shall be tidy)
package persist

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
)

// Store for all persistence documents. Each document has a key associated with it,
// usually this is a path or a path-like string referring to the document. Note that
// not all persistence is necessarily file-system based and depends on how the source
// is implemented.
type Store interface {

	// Open opens a Document for reading. If successful, methods on
	// the returned Document can be used for reading; it will have its mode set
	// to BasicOpenMode, which specifies read-only.
	//
	// If fqAltKey is non-empty, it will be used as the key instead of key; in
	// this case it must be a properly-formatted fully-qualified alternative key
	// as specified by the implementor.
	Open(key, fqAltKey string) (Document, error)

	// OpenDocument is the generalized open call; most users will use Open or
	// Create instead. If the Document does not exist, and mode.Create is set
	// to true, the Document is created. If successful, methods on the returned
	// Document can be used for I/O.
	OpenDocument(key, fqAltKey string, mode DocumentMode) (Document, error)

	// Create creates or truncates a Document. If the Document already exists,
	// it is truncated. If the Document does not already exist, it is created.
	// If successful, methods on the returned Document can be used for IO.
	//
	// If fqAltKey is non-empty, it will be used as the key instead of key; in
	// this case it must be a properly-formatted fully-qualified alternative key
	// as specified by the implementor.
	Create(key, fqAltKey string) (Document, error)
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
