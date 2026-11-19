# objectstore

> Language: [简体中文](README.md) · [English](README.en.md)

A Go object-storage abstraction: one `Backend` interface with three interchangeable backends — **local volume**, **S3-compatible services** (AWS S3 / Cloudflare R2 / MinIO / RustFS, etc.), and **in-process memory**. Application code deals only in object keys — no file paths, signed URLs, or SDK calls. Switching backends is a config change, not a code change.

Designed for artifact storage where data must survive container restarts and machine changes: write outputs such as scraped data, transcodes, and thumbnails as object keys — use a directory in local development and an S3-compatible service in production.

## Installation

```bash
go get github.com/NoelOrin/objectstore
```

Requires Go 1.27+ (the S3 backend depends on AWS SDK v2).

## Quick start

```go
import (
    "context"
    "os"

    "github.com/NoelOrin/objectstore"
)

func newStore() (objectstore.Backend, error) {
    backend, err := objectstore.Open(objectstore.Config{
        Driver:     "s3", // "" / "local" / "s3" / "memory"
        S3Endpoint: os.Getenv("S3_ENDPOINT"),
        S3Bucket:   os.Getenv("S3_BUCKET"),
        S3AccessKey: os.Getenv("S3_ACCESS_KEY"),
        S3SecretKey: os.Getenv("S3_SECRET_KEY"),
        S3Prefix:   "app/prod",
    })
    if err != nil {
        return nil, err
    }
    // Probe at startup: fail fast on misconfiguration instead of waiting for the first request.
    if err := backend.TestConnection(context.Background()); err != nil {
        return nil, err
    }
    return backend, nil
}

func saveAndRead(ctx context.Context, backend objectstore.Backend, data []byte) ([]byte, string, error) {
    if err := backend.Put(ctx, "metadata/abc/lyrics.lrc", data, "text/plain; charset=utf-8"); err != nil {
        return nil, "", err
    }
    out, mime, err := backend.Get(ctx, "metadata/abc/lyrics.lrc")
    return out, mime, err
}
```

## The `Backend` interface

| Method | Behavior |
| --- | --- |
| `Put(ctx, key, data, contentType)` | Overwrite. `local` writes a temp file in the same directory and then `rename`s it, so no partially written files are left behind |
| `Get(ctx, key)` | Returns `([]byte, contentType, error)`; when the object is missing, `errors.Is(err, objectstore.ErrNotFound)` |
| `Delete(ctx, key)` | Deletes an object; **deleting a missing object is not an error** (idempotent) |
| `Stat(ctx, key)` | Returns the size in bytes and the modification time (Unix milliseconds) |
| `List(ctx, prefix)` | Object keys under a prefix, in **lexicographic order**; S3 pagination is handled automatically |
| `TestConnection(ctx)` | Health probe: `local` writes a probe file, `s3` calls `HeadBucket` |
| `Name()` | Returns the current backend name (`local` / `s3` / `memory`) for logging |

Keys always use forward slashes (`a/b/c.jpg`); each backend maps them to a local path or an S3 object key.

## Configuration

| Field | Description |
| --- | --- |
| `Driver` | `""`/`local` (default), `s3`, `memory`; unknown values fail immediately |
| `LocalDir` | Local root directory; defaults to `./metadata` |
| `S3Endpoint` / `S3Bucket` | Required for the S3 backend |
| `S3Region` | Defaults to `us-east-1` |
| `S3AccessKey` / `S3SecretKey` | Static credentials; leave empty to use the SDK's default credential chain |
| `S3Prefix` | A uniform object-key prefix (e.g. `app/prod`), automatically prepended on reads and writes |
| `S3ForcePathStyle` | When `true`, forces path-style addressing; automatically enabled by default for non-AWS endpoints |
| `PublicURL` / `S3PublicBaseURL` | Only for callers assembling public URLs; the library itself does not use them (server-side reads/writes never go over the public network) |

## Three backends

- **local**: Object keys are `filepath.Clean`ed with `/` as the root before being joined to the root directory, so `../` cannot escape it (covered by dedicated tests). Writes use a temp file + `rename` for atomic replacement. Good for single machines and container-mounted volumes.
- **s3**: AWS SDK v2; works with any S3-compatible service. Path-style addressing is enabled automatically for non-AWS endpoints; every operation has a 10s timeout; `S3Prefix` is prepended to keys automatically. Good for persistence that must be shared across instances.
- **memory**: In-process, lost on restart — for tests only.

```go
// Equivalent to local, reading ./metadata
b, _ := objectstore.Open(objectstore.Config{})
```

## Errors and semantics

- A missing object has exactly one unambiguous signal: `errors.Is(err, objectstore.ErrNotFound)` (`Get`/`Stat`). Do not compare strings.
- `Delete` is idempotent; `List` takes no pagination parameter — callers narrow results by prefix themselves.
- `Open` only constructs the backend and makes no network calls; `TestConnection` is the actual health probe — call it once at startup.

## Testing

```bash
go test ./...
```

The `local` and `memory` backends are always tested (they share the same `Backend` contract). Contract tests against a real S3-compatible service are enabled via environment variables:

| Variable | Required | Description |
| --- | --- | --- |
| `OBJECTSTORE_TEST_S3_ENDPOINT` | Yes | e.g. `https://accountid.r2.cloudflarestorage.com` |
| `OBJECTSTORE_TEST_S3_BUCKET` | Yes | The test bucket |
| `OBJECTSTORE_TEST_S3_REGION` | No | Defaults to `us-east-1` |
| `OBJECTSTORE_TEST_S3_ACCESS_KEY` / `OBJECTSTORE_TEST_S3_SECRET_KEY` | No | Leave empty to use the default credential chain |

When unset, the test explicitly calls `t.Skip("no S3 test credentials")` — it does not silently pass. The legacy `MUSIC_TEST_S3_*` names are still supported.
