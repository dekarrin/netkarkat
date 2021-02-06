package persist

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// fsSourceStore is a store that can open files on the filesystem in a particular directory;
// all keys are paths relative to that directory. Optionally, its fully-qualified
// alt key can be used to specifify an absolute path to another file on disk.
type fsSourceStore struct {
	dir          string
	newFilePerms os.FileMode
	enc          func(i interface{}) error
	dec          func(i interface{}) error
}

type fileDocument struct {
	f      *os.File
	closed bool
	mode   DocumentMode

	key  string
	fqak bool

	readBuf  *bufio.Reader
	writeBuf bytes.Buffer
}

// Read reads bytes from the file.
func (fDoc *fileDocument) Read(b []byte) (n int, err error) {
	if fDoc.closed {
		return 0, fmt.Errorf("Document has been closed and cannot perform further operations")
	}
	if fDoc.mode.AllowedOperations == WriteOnly {
		return 0, fmt.Errorf("Document opened in write-only mode and cannot perform reads")
	}
	if fDoc.mode.Synchronous {
		return fDoc.f.Read(b)
	}
	return fDoc.readBuf.Read(b)
}

// Write writes bytes to the document.
func (fDoc *fileDocument) Write(b []byte) (n int, err error) {
	if fDoc.closed {
		return 0, fmt.Errorf("Document has been closed and cannot perform further operations")
	}
	if fDoc.mode.AllowedOperations == ReadOnly {
		return 0, fmt.Errorf("Document opened in read-only mode and cannot perform writes")
	}
	if fDoc.mode.Synchronous {
		return fDoc.f.Write(b)
	}
	return fDoc.writeBuf.Write(b)
}

// Seek moves the cursor position to the given offset. If opened in Append mode,
// this will have no effect on future writes.
func (fDoc *fileDocument) Seek(offset int64, whence int) (n int64, err error) {
	if fDoc.closed {
		return 0, fmt.Errorf("Document has been closed and cannot perform further operations")
	}

	return fDoc.f.Seek(offset, whence)
}

// Close flushes all currently written to the Document to the actual
// backing store (if in asynchronous mode) and closes any open resources
// associated with the Document. It will not be able to be used after
// Close has been called, regardless of whether error is non-nil.
//
// Every call to Close() after the first will have no effect and will return
// a nil error.
func (fDoc *fileDocument) Close() (err error) {
	if fDoc.closed {
		return nil // already closed, don't need to do it again
	}

	var flushErr error
	if fDoc.mode.AllowedOperations != ReadOnly && !fDoc.mode.Synchronous {
		// do not need to flush writes if it's read only (where there will not
		// be any valid writes) or synchronous (which auto flushes).
		flushErr = fDoc.Flush()
	}

	closeErr := fDoc.f.Close()

	fDoc.closed = true
	if closeErr != nil && flushErr != nil {
		return fmt.Errorf("%v; additionally, while flushing remaining write buffer: %v", closeErr, flushErr)
	}
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

// Flush flushes all pending writes.
func (fDoc *fileDocument) Flush() (err error) {
	if fDoc.closed {
		return fmt.Errorf("Document has been closed and cannot perform further operations")
	}

	// no need to check the read-only-edness since that will be handled by simply never allowing
	// any writes to hit the buffer.

	if fDoc.mode.Synchronous {
		// all writes are unbuffered; nothing to flush
		return nil
	}
	if fDoc.writeBuf.Len() < 1 {
		// nothing to flush
		return nil
	}

	fileWriter := bufio.NewWriter(fDoc.f)
	n, err := fileWriter.Write(fDoc.writeBuf.Bytes())
	if err != nil {
		return fmt.Errorf("after writing %d bytes to secondary buffer, got: %v", n, err)
	}

	if err := fileWriter.Flush(); err != nil {
		return err
	}

	fDoc.writeBuf = bytes.Buffer{}
	return nil
}

// Mode gets the DocumentMode that the fileDocument was opened with.
func (fDoc *fileDocument) Mode() DocumentMode {
	return fDoc.mode
}

// Key gets the path to the file. It may be relative to a parent directory; if
// it is, UsesAlternativeKey() returns true.
func (fDoc *fileDocument) Key() string {
	return fDoc.key
}

// UsesAlternativeKey returns whether the key returned by Key() is a
// fully-qualified alternative key.
func (fDoc *fileDocument) UsesAlternativeKey() bool {
	return fDoc.fqak
}

// NewFilesystemStore creates and returns a new Store that reads/writes Documents
// as files on the filesystem, all relative to a given directory. The given directory
// will be created if needed.
//
// dirPerm and newFilePerm are permissions flags; dirPerm is what permissions
// mask to create the directory with (if it needs to be created), and
// newDocPerm is what permissions new Document files in the store are created as.
// Only the permissions portion is used; all other aspects of os.FileMode are
// ignored. Both of these values can be set a default by the caller setting them
// to nil. If dirPerm is set to nil, the newly created directed is created with
// permissions mask 0666. If newDocPerm is set to nil, newly-created
// document files are created with permissions mask 0666.
//
// err will be non-nil when the directory could not be accessed or created.
func NewFilesystemStore(directory string, dirPerm, newDocPerm *os.FileMode) (store Store, err error) {
	fsStore := &fsSourceStore{
		dir:          directory,
		newFilePerms: 0666,
	}
	if newDocPerm != nil {
		fsStore.newFilePerms = newDocPerm.Perm()
	}

	if info, err := os.Stat(directory); err != nil {
		if os.IsNotExist(err) {
			dirMode := os.ModeDir | 0666
			if dirPerm != nil {
				dirMode = os.ModeDir | dirPerm.Perm()
			}
			if err = os.Mkdir(directory, dirMode); err != nil && !os.IsExist(err) {
				return nil, err
			}
			return fsStore, nil
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("path exists and is not a directory")
		}
	}
	return store, nil
}

// NewUserHomeDirStore creates and returns a new Store that reads/writes Documents
// as files on the filesystem, all relative to a given directory which itself is relative
// to the user's home directory.
//
// dirPerm and newDocPerm are the same as in NewFilesystemStore().
func NewUserHomeDirStore(directory string, dirPerm, newDocPerm *os.FileMode) (Store, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("couldn't open user homedir: %v", err)
	}
	appDir := filepath.Join(homedir, directory)
	return NewFilesystemStore(appDir, dirPerm, newDocPerm)
}

func (fsStore *fsSourceStore) OpenDocument(key string, mode DocumentMode) (doc Document, err error) {
	return fsStore.OpenDocumentAlt(key, "", mode)
}

func (fsStore *fsSourceStore) Open(key string) (doc Document, err error) {
	return fsStore.OpenDocument(key, BasicOpenMode)
}

func (fsStore *fsSourceStore) Create(key string) (doc Document, err error) {
	return fsStore.OpenDocument(key, BasicCreateMode)
}

func (fsStore *fsSourceStore) OpenDocumentAlt(key, fqAltKey string, mode DocumentMode) (doc Document, err error) {
	fDoc := &fileDocument{
		mode: mode,
		key:  key,
	}

	path := fqAltKey
	if path != "" {
		fDoc.fqak = true
		fDoc.key = fqAltKey
	} else {
		path = filepath.Join(fsStore.dir, key)
	}

	flags := fileFlagsFromDocumentMode(mode)
	fDoc.f, err = os.OpenFile(path, flags, fsStore.newFilePerms)
	if err != nil {
		return nil, err
	}

	if !mode.Synchronous {
		fDoc.readBuf = bufio.NewReader(fDoc.f)
	}

	return fDoc, nil
}

func (fsStore *fsSourceStore) OpenAlt(key, fqAltKey string) (doc Document, err error) {
	return fsStore.OpenDocumentAlt(key, fqAltKey, BasicOpenMode)
}

func (fsStore *fsSourceStore) CreateAlt(key, fqAltKey string) (doc Document, err error) {
	return fsStore.OpenDocumentAlt(key, fqAltKey, BasicCreateMode)
}

func fileFlagsFromDocumentMode(mode DocumentMode) int {
	var flags int

	switch mode.AllowedOperations {
	case ReadOnly:
		flags = os.O_RDONLY
	case WriteOnly:
		flags = os.O_WRONLY
	case ReadAndWrite:
		flags = os.O_RDWR
	default:
		panic(fmt.Sprintf("unrecognized AllowedOperations code: %v", mode.AllowedOperations))
	}

	if mode.Append {
		flags |= os.O_APPEND
	}
	if mode.Create {
		flags |= os.O_CREATE
	}
	if mode.Exclusive {
		flags |= os.O_EXCL
	}
	if mode.Synchronous {
		flags |= os.O_SYNC
	}
	if mode.Truncate {
		flags |= os.O_TRUNC
	}
	return flags
}
