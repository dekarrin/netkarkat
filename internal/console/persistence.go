package console

import (
	"bufio"
	"dekarrin/netkarkat/internal/persist"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
)

// source for persistence in case we want to change it up later
//
// currently might seen v silly since all we have is "directory, with files",
// but that may change in the future.
//
// (probs yagni but fuck it this is my house and my house shall be tidy)
type persistSource struct {
	dirBased string
}

func (state *consoleState) loadPersistenceFiles() {
	var err error
	state.userStore, err = persist.NewUserHomeDirStore(".netkk", nil, nil)
	if err != nil {

	}
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
