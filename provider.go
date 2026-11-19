// Package objectstore is the storage abstraction for scraped metadata assets.
// It follows GOSpeak's app/server/internal/storage pattern: one small provider
// interface, a local-disk backend for development/containers, and an S3-compatible
// backend for durable production storage. Application code never sees a path or
// an SDK call — only object keys.
package objectstore

import (
	"context"
	"errors"
	"time"
)

func nowMS() int64 { return time.Now().UnixMilli() }

// ErrNotFound is returned by Get/Stat when an object does not exist. Callers
// should use errors.Is rather than comparing the error text.
var ErrNotFound = errors.New("object not found")

// Backend stores and retrieves objects by key. Keys use forward slashes on
// every backend; implementations map them to local paths or S3 object keys.
type Backend interface {
	// Put overwrites the object at key.
	Put(ctx context.Context, key string, data []byte, contentType string) error
	// Get returns the object bytes and its content type.
	Get(ctx context.Context, key string) ([]byte, string, error)
	// Delete removes an object; deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// Stat returns the byte size and modification time (Unix milliseconds).
	Stat(ctx context.Context, key string) (size, updatedAtMS int64, err error)
	// List returns object keys under prefix, sorted lexicographically.
	List(ctx context.Context, prefix string) ([]string, error)
	// TestConnection verifies the backend is reachable and writable.
	TestConnection(ctx context.Context) error
	// Name identifies the active backend ("local", "s3", "memory").
	Name() string
}

// Config selects and configures a backend. It mirrors GOSpeak's storage
// environment variables: STORAGE_DRIVER / STORAGE_LOCAL_DIR / STORAGE_S3_*.
type Config struct {
	Driver    string // "local" (default), "s3" or "memory"
	LocalDir  string
	PublicURL string // optional, not used for server-side reads

	S3Endpoint       string
	S3Bucket         string
	S3Region         string
	S3AccessKey      string
	S3SecretKey      string
	S3Prefix         string
	S3PublicBaseURL  string
	S3ForcePathStyle bool
}

// Open builds a backend from cfg. An empty driver means "local".
func Open(cfg Config) (Backend, error) {
	switch cfg.Driver {
	case "", "local":
		return NewLocal(cfg.LocalDir), nil
	case "memory":
		return NewMemory(), nil
	case "s3":
		return NewS3(cfg)
	default:
		return nil, errors.New("objectstore: unknown driver " + cfg.Driver)
	}
}
