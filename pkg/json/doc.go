package json

import (
	"io"
	"time"

	"github.com/open-policy-agent/eopa/pkg/json/internal/utils"
)

type Kind uint

const (
	Invalid Kind = iota
	Directory
	Unstructured // Unstructured is reserved for binary resource.
	JSON
)

// File is the interface all files (structured or unstructured) stored to the snapshot implement. It is for accessing the contents of the file only; the
// metadata is to be accessed over the Resource interface (which represents a single entity in the namespace).
type File interface {
	io.WriterTo
	Contents() interface{}

	// Clone returns a (deep) copy of the file.
	Clone(deepCopy bool) File
}

// Collections represents a logical snapshot of hierarchical resources coming from a single data source. Internally it is binary encoded, and it may use delta encoding for compact representation.
type Collections interface {
	// Resource returns resource meta data for a particular resource. Returns nil if no resource matches the name.
	Resource(name string) Resource

	// Collections returns all JSON collections available.
	Collections() []string

	// Walk executes a depth-first search over the snapshot, stopping the recursion (but not the entire walk) if the callback returns false.
	Walk(func(resource Resource) bool)

	// Diff computes a binary diff against collections provided and returns a reader for consuming the computed diff together with the number of bytes available. Boolean value is true
	// if the diff is empty.
	Diff(other Collections) (*utils.BytesReader, int64, bool, error)

	// Writable returns a writable copy of the collections.
	Writable() WritableCollections

	// Len returns the # of bytes in binary level representation.
	Len() int64

	// Reader returns a binary level reader for the entire collection.
	Reader() *utils.MultiReader

	// DeltaReader returns a binary level reader for the delta of the collection, if a delta collection.
	DeltaReader() *utils.MultiReader

	io.WriterTo

	// Objects returns the storage objects below. 	If a snapshot based collection, the slice will hold only one entry, the snapshot object. If a
	// delta based collection, the first entity will be the delta object and the second for the snapshot. Note the meta data may be nil, if it was not provided at the construction time.
	Objects() []interface{}

	// Write operations. With binary collections they operate on the deltas.

	// WriteBlob replaces the contents of a binary resource.
	WriteBlob(name string, blob Blob)

	// WriteJSON replaces the contents of a JSON resource.
	WriteJSON(name string, j Json)

	// PatchJSON applies a patch to a JSON resource.
	PatchJSON(name string, patch Patch) (bool, error)

	// WriteDirectory creates a directory.
	WriteDirectory(name string)

	// Remove removes a resource. It returns true if found and successfully removed. It does not remove a directory if it's not empty.
	Remove(name string) bool

	// Writes a meta key-value pair for a resource, returning if successful. Note, the resource has to exist this to take effect.
	WriteMeta(name string, key string, value string) bool
}

// WritableCollections is updateable logical snapshot.
type WritableCollections interface {
	// Blob returns resource meta data for a particular resource. Returns nil if no resource matches the name.
	Resource(name string) Resource

	// WriteBlob replaces the contents of a
	WriteBlob(name string, blob Blob)

	// WriteJSON
	WriteJSON(name string, j Json)

	// PatchJSON applies a patch to a JSON resource.
	PatchJSON(name string, patch Patch) (bool, error)

	// WriteDirectory
	WriteDirectory(name string)

	// Writes a meta key-value pair for a resource, returning if successful. Note, the resource has to exist this to take effect.
	WriteMeta(name string, key string, value string) bool

	// Prepare prepares the collection for log append. Any changes to the writable collection after this invocation are not reflected to the returned collections.
	Prepare(timestamp time.Time) Collections
}

// Resource captures the meta data available for a resource. It is limited to type information for now.
type Resource interface {
	// Name returns the full name.
	Name() string

	// Kind returns Invalid if the resource does not exist, otherwise Unstructured (for a binary resource), JSON (for a JSON resource), and Directory for directory.
	Kind() Kind

	// Blob returns resource meta data for a particular resource. Provided name is local and scoped to the directory. Returns nil if no resource matches the name.
	Resource(name string) Resource

	// Resources returns all resources under a directory (not recursively, though). Will panic if the name does not point to a directory resource.
	Resources() []Resource

	// File returns a file object. It's callers responsibility to determine the type.
	File() File

	// Blob returns a binary resource. Will panic if the resource is not of binary type.
	Blob() Blob

	// Collection returns a JSON collection resource. Will panic if the resource is not of JSON type.
	JSON() Json

	// Meta returns a meta value for the key, and true if it exists.
	Meta(key string) (string, bool)

	// Walk executes a depth-first search over the resource, stopping the recursion to a particular node if the callback returns false but not the entire walk.
	Walk(callback func(Resource) bool)
}
