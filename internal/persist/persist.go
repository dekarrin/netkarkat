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
// for a specific key in the store. The Alt-variants of these allow specifying
// an alternate "fully-qualified" key that is retrieved instead of the main key,
// if the FQAK is non-zero. The meaning of "fully-qualified" is different
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

// Store for all persistence documents. Each document has a key associated with it,
// usually this is a path or a path-like string referring to the document. Note that
// not all persistence is necessarily file-system based and depends on how the source
// is implemented.
type Store interface {

	// Open opens a Document for reading. If successful, methods on
	// the returned Document can be used for reading; it will have its mode set
	// to BasicOpenMode, which specifies read-only.
	Open(key string) (Document, error)

	// OpenDocument is the generalized open call; most users will use Open or
	// Create instead. If the Document does not exist, and mode.Create is set
	// to true, the Document is created. If successful, methods on the returned
	// Document can be used for I/O.
	OpenDocument(key string, mode DocumentMode) (Document, error)

	// Create creates or truncates a Document. If the Document already exists,
	// it is truncated. If the Document does not already exist, it is created.
	// If successful, methods on the returned Document can be used for IO.
	Create(key string) (Document, error)

	// OpenAlt opens a Document for reading. If successful, methods on
	// the returned Document can be used for reading; it will have its mode set
	// to BasicOpenMode, which specifies read-only.
	//
	// If fqAltKey is non-empty, it will be used as the key instead of key; in
	// this case it must be a properly-formatted fully-qualified alternative key
	// as specified by the implementor.
	//
	// To enable Encode and Decode on the returned document, pass in a non-nil
	// Codec. If Encode/Decode support is not needed, nil can be given for the
	// codec.
	OpenAlt(key, fqAltKey string) (Document, error)

	// OpenDocumentAlt is the generalized open call; most users will use OpenAlt
	// or CreateAlt instead. If the Document does not exist, and mode.Create is
	// set to true, the Document is created. If successful, methods on the
	// returned Document can be used for I/O.
	//
	// If fqAltKey is non-empty, it will be used as the key instead of key; in
	// this case it must be a properly-formatted fully-qualified alternative key
	// as specified by the implementor.
	//
	// To enable Encode and Decode on the returned document, pass in a non-nil
	// Codec. If Encode/Decode support is not needed, nil can be given for the
	// codec.
	OpenDocumentAlt(key, fqAltKey string, mode DocumentMode) (Document, error)

	// CreateAlt creates or truncates a Document. If the Document already
	// exists, it is truncated. If the Document does not already exist, it is
	// created. If successful, methods on the returned Document can be used for
	// IO.
	//
	// If fqAltKey is non-empty, it will be used as the key instead of key; in
	// this case it must be a properly-formatted fully-qualified alternative key
	// as specified by the implementor.
	//
	// To enable Encode and Decode on the returned document, pass in a non-nil
	// Codec. If Encode/Decode support is not needed, nil can be given for the
	// codec.
	CreateAlt(key, fqAltKey string) (Document, error)
}
