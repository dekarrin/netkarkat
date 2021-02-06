package persist

import (
	"fmt"
	"io"
)

// Document is a document that can be read or written to. Changes do not persist on write,
// only on flush, unless Synchronous was given as a mode flag.
type Document interface {
	io.ReadWriteCloser
	io.Seeker

	// Flush immediately commits all pending writes. Not used if in synchronous mode.
	Flush() error

	// Mode gives the DocumentMode that this Document was created with.
	Mode() DocumentMode

	// Key gives the key used to refer to the Document in its source document
	// store. It will be the key that it was created with. If the Document was
	// created by selecting a fully-qualified alternative key, Key() will return
	// that fqAlt key and KeyIsAlternative() will return true.
	Key() string

	// UsesAlternativeKey returns whether the string returned by Key() is valid
	// in the context of a full-qualified alternative key. If false, it is
	// a "normal" key.
	UsesAlternativeKey() bool
}

// AllowedOperations is a number that specifies which operations
// are allowed on a Document.
type AllowedOperations int

const (
	// ReadAndWrite indicates that both writing and reading is allowed on the Document.
	// It is the default if no other AllowedOperations flag is set.
	ReadAndWrite AllowedOperations = iota
	// ReadOnly indicates that only reading is allowed on the Document.
	ReadOnly
	// WriteOnly indicates that only writing is allowed on the Document.
	WriteOnly
)

func (ao AllowedOperations) String() string {
	switch ao {
	case ReadOnly:
		return "ReadOnly"
	case WriteOnly:
		return "WriteOnly"
	case ReadAndWrite:
		return "ReadAndWrite"
	default:
		return fmt.Sprintf("UnknownOperation(%d)", int(ao))
	}
}

var (
	// BasicOpenMode indicates that the Document should be opened as read-only,
	// and specifies no other options. It is the mode used by Store.Open().
	BasicOpenMode = DocumentMode{AllowedOperations: ReadOnly}

	// BasicCreateMode indicates that the Document should be opened as
	// read-write, that it should be created if it does not exist, and that any
	// existing Document at that key is to be truncated to 0. It is the mode
	// used by Store.Create().
	BasicCreateMode = DocumentMode{AllowedOperations: ReadAndWrite, Create: true, Truncate: true}
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

	// Exclusive is used with Create and specifies that there must not already be
	// a document that exists during a create operation.
	Exclusive bool

	// Synchronous indicates that synchronous IO will be used; in this case writes to
	// the Document are persisted immediately, and reads are made in a non-synchronous
	// fashion.
	Synchronous bool

	// Truncate indicates that on the open of a Document, any existing document at the key
	// is to be immediately truncated to 0 bytes. If there isn't already an existing document,
	// this flag will have no effect.
	Truncate bool
}

// WithTruncate returns a new DocumentMode identical to the current one but
// with the Truncate flag set to the given value.
func (dm DocumentMode) WithTruncate(b bool) DocumentMode {
	dm.Truncate = b
	return dm
}

// WithSynchronous returns a new DocumentMode identical to the current one but
// with the Truncate flag set to the given value.
func (dm DocumentMode) WithSynchronous(b bool) DocumentMode {
	dm.Synchronous = b
	return dm
}

// WithExclusive returns a new DocumentMode identical to the current one but
// with the Exclusive flag set to the given value.
func (dm DocumentMode) WithExclusive(b bool) DocumentMode {
	dm.Exclusive = b
	return dm
}

// WithCreate returns a new DocumentMode identical to the current one but
// with the Create flag set to the given value.
func (dm DocumentMode) WithCreate(b bool) DocumentMode {
	dm.Create = b
	return dm
}

// WithAppend returns a new DocumentMode identical to the current one but
// with the Append flag set to the given value.
func (dm DocumentMode) WithAppend(b bool) DocumentMode {
	dm.Append = b
	return dm
}

// WithAllowedOps returns a new DocumentMode identical to the current one but
// with the allowed operations set to the given value.
func (dm DocumentMode) WithAllowedOps(a AllowedOperations) DocumentMode {
	dm.AllowedOperations = a
	return dm
}
