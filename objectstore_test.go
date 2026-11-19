package objectstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testBackend runs the same contract against every backend. S3 is included when
// OBJECTSTORE_TEST_S3_* is set, so the file-based backends stay testable offline.
// testEnv reads OBJECTSTORE_TEST_S3_<suffix>, falling back to the historical
// MUSIC_TEST_S3_<suffix> name so existing CI setups keep working.
func testEnv(suffix string) string {
	if v := os.Getenv("OBJECTSTORE_TEST_S3_" + suffix); v != "" {
		return v
	}
	return os.Getenv("MUSIC_TEST_S3_" + suffix)
}

func testBackend(t *testing.T, b Backend) {
	t.Helper()
	ctx := context.Background()
	if err := b.TestConnection(ctx); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if err := b.Put(ctx, "metadata/abc/lyrics.lrc", []byte("[00:01.00]hi"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	data, mime, err := b.Get(ctx, "metadata/abc/lyrics.lrc")
	if err != nil || string(data) != "[00:01.00]hi" || mime == "" {
		t.Fatalf("Get = %q, %q, %v", data, mime, err)
	}
	size, updated, err := b.Stat(ctx, "metadata/abc/lyrics.lrc")
	if err != nil || size != int64(len(data)) || updated <= 0 {
		t.Fatalf("Stat = %d, %d, %v", size, updated, err)
	}
	if err := b.Put(ctx, "metadata/abc/cover.jpg", []byte{1, 2, 3}, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	keys, err := b.List(ctx, "metadata/abc")
	if err != nil || len(keys) != 2 {
		t.Fatalf("List = %v, %v", keys, err)
	}
	if err := b.Delete(ctx, "metadata/abc/lyrics.lrc"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Get(ctx, "metadata/abc/lyrics.lrc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing object error = %v, want ErrNotFound", err)
	}
}

func TestLocalBackend(t *testing.T) {
	testBackend(t, NewLocal(t.TempDir()))
}

func TestMemoryBackend(t *testing.T) {
	testBackend(t, NewMemory())
}

func TestLocalContainsPathTraversal(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "store")
	b := NewLocal(root)
	// Keys are cleaned against a "/" root before joining, so "../escape"
	// normalizes to "escape" inside the store and can never write to parent.
	if err := b.Put(context.Background(), "../escape", []byte("x"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); err != nil {
		t.Fatalf("object not contained in store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "escape")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("object escaped the store root: %v", err)
	}
}

func TestS3BackendWhenConfigured(t *testing.T) {
	endpoint := testEnv("ENDPOINT")
	bucket := testEnv("BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("no S3 test credentials")
	}
	b, err := NewS3(Config{
		S3Endpoint: endpoint, S3Bucket: bucket,
		S3Region:         testEnv("REGION"),
		S3AccessKey:      testEnv("ACCESS_KEY"),
		S3SecretKey:      testEnv("SECRET_KEY"),
		S3ForcePathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	testBackend(t, b)
}
