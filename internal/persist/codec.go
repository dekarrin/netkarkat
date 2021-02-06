package persist

import (
	"encoding/gob"
	"fmt"
	"reflect"
)

// Codec is a type that knows how to encode and decode. Prepare() is called
// with the document that will be operated on, and then Decode(), Encode(), and
// Skip() can be used to read and write the data in it in the format supported
// by the Codec. For Documents that are writable, Finalize() is called when the
// Document is closed to finish the encoding process.
//
// The Zero-value of all Codecs are assumed to be usable.
type Codec interface {
	// Fromat returns the human-readable name of the format that the Codec works
	// with.
	Format() string

	// Prepare sets up the Codec for use on the given Document. Further calls to
	// Decode and Encode will operate on this Document, and Finalize() will
	// complete the operation and release the associated resources.
	//
	// If Prepare() has already been called on a previous Document, the previous
	// Document is replaced with the new one. Finalize() is not called
	// automatically, so if the Codec requires a call to Finalize() to persist
	// its encoding, this could result in lost data.
	Prepare(doc Document) error

	// Decode reads the next value from the Document input stream and stores it
	// in the passed-in empty interface value. The value underlying i must be a
	// pointer to the correct type for the next data item received. Decode
	// requires that Prepare() has been called at least once.
	//
	// If v is nil, Decode returns a non-nil error and does not modify v. If the
	// Document is already at EOF, Decode returns io.EOF and does not modify v.
	// If Prepare has not been called at least once, Decode returns a non-nil
	// error and does not modify v.
	//
	// Decode cannot be used to read a value and then discard it without giving
	// type information on the discarded value; for this functionality, see
	// Discard().
	Decode(v interface{}) error

	// Encode encodes the data item represented by the empty interface value.
	// Encode requires that Prepare() has been called at least once; if not,
	// Encode returns a non-nil error.
	//
	// The empty interface value is allowed to be nil if the implementing codec
	// allows for the encoding of nil values; if it does not, passing nil to
	// Encode will return a non-nil error.
	//
	// Some Codecs may write the encoded value immediately to the document,
	// while others will require Finalize() to be called due to needing to
	// perform post-completion steps on a Document that does not support Seek().
	Encode(v interface{}) error

	// Discard reads the next data value in the stream and discards it. This may
	// be unsupported by Codecs whose format does not allow skipping of records
	// without out-of-band type information; in this case, Discard returns a
	// non-nil error. Decode() could be used with an appropriately-typed
	// pointer passed in to achieve similar functionality.
	//
	// What counts as an "item" is dependent on the underlying format.
	Discard() error

	// Finalize performs all post-completion steps and releases any resources
	// that were set up in Prepare().
	//
	// The exact post-completion steps taken depends on the requirements of the
	// format that the Codec works with, and may include index creation, header
	// data, or encryption. After any such steps are complete, any resources
	// that were set up for encoding or decoding to the document passed in to
	// Prepare() are removed.
	//
	// After Finalize is called, subsequent calls prior to calling Prepare()
	// will have no effect and will return a nil error.
	Finalize() error
}

type codecUserMixin struct {
	codecs []Codec
}

// UseCodec begins using the given codex for future calls to Encode, Decode, and
// Discard.
func (mix *codecUserMixin) UseCodec(c Codec) {
	mix.codecs = append(fDoc.codecs, c)
	return fDoc
}

// Encode encodes the given interface value using the current set of codecs.
func (fDoc *fileDocument) Encode(i interface{}) error {
	if len(fDoc.codecs) < 1 {
		return fmt.Errorf("no codecs to encode with; call UseCodec() first")
	}
	return fDoc
}

// GobCodec is used to work with Gob-formated data in a Document. The zero value
// is ready to use.
type GobCodec struct {
	enc *gob.Encoder
	dec *gob.Decoder
}

// Format returns "gob", the name of the format that the GobCodec works with.
func (gobber *GobCodec) Format() string {
	return "gob"
}

// Prepare readies the GobCodec for use with the given Document.
func (gobber *GobCodec) Prepare(doc Document) error {
	gobber.enc = gob.NewEncoder(doc)
	gobber.dec = gob.NewDecoder(doc)
	return nil
}

// Decode decodes data from the Document in GOB-format.
func (gobber *GobCodec) Decode(v interface{}) error {
	if gobber.dec == nil {
		return fmt.Errorf("no document to decode; call Prepare() first")
	}
	if v == nil {
		return fmt.Errorf("cannot decode to nil; use Skip() if trying to discard")
	}
	return gobber.dec.Decode(v)
}

// Encode encodes data to the Document in GOB-format.
func (gobber *GobCodec) Encode(v interface{}) error {
	if gobber.enc == nil {
		return fmt.Errorf("no document to encode to; call Prepare() first")
	}
	if v == nil {
		return fmt.Errorf("GOB format does not support encoding nil pointers")
	}
	return gobber.enc.Encode(v)
}

// Discard skips the next data item in the Document in GOB-format.
func (gobber *GobCodec) Discard() error {
	if gobber.dec == nil {
		return fmt.Errorf("no document to decode; call Prepare() first")
	}

	return gobber.dec.DecodeValue(reflect.Value{})
}

// Finalize disassociates from the Document passed in Prepare().
func (gobber *GobCodec) Finalize() error {
	gobber.dec = nil
	gobber.enc = nil
	return nil
}
