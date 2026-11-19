// Package objectstore is a small storage abstraction for service-generated
// assets: one Backend interface, a local-disk backend for development and
// containers, an S3-compatible backend for durable production storage, and an
// in-memory backend for tests. Application code never sees a path or an SDK
// call — only object keys.
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

// PresignedUpload describes a presigned HTTP request for a direct client upload.
// It carries no application-specific fields: callers decide what to do with the
// returned URL and key.
type PresignedUpload struct {
	URL       string
	Method    string
	ObjectKey string
	ExpiresAt time.Time
}

// Presigner is an optional capability for backends that support direct client
// uploads. It is deliberately separate from Backend so existing implementations
// stay source-compatible. Assert a Backend to Presigner to check support.
type Presigner interface {
	PresignUpload(ctx context.Context, key string, contentType string, size int64, expires time.Duration) (*PresignedUpload, error)
}

// PublicURLer is an optional capability for backends that can derive a
// public read URL from an object key. It stays separate from Backend so
// existing implementations remain source-compatible.
type PublicURLer interface {
	PublicURL(key string) string
}

// Config selects and configures a backend. Callers map their own environment
// variables onto these fields; the library reads no environment itself.
type Config struct {
	Driver   string // "local" (default), "s3" or "memory"
	LocalDir string
	// PublicURL is the public base for the local backend's PublicURL().
	PublicURL string

	S3Endpoint       string
	S3Bucket         string
	S3Region         string
	S3AccessKey      string
	S3SecretKey      string
	S3Prefix         string
	S3PublicBaseURL  string
	S3ForcePathStyle bool
	S3Timeout        time.Duration // per-operation timeout, default 10s
	S3PresignTTL     time.Duration // default presign lifetime, default 5m
}

// Open builds a backend from cfg. An empty driver means "local".
func Open(cfg Config) (Backend, error) {
	switch cfg.Driver {
	case "", "local":
		b := NewLocal(cfg.LocalDir)
		b.publicURL = cfg.PublicURL
		return b, nil
	case "memory":
		return NewMemory(), nil
	case "s3":
		return NewS3(cfg)
	default:
		return nil, errors.New("objectstore: unknown driver " + cfg.Driver)
	}
}
