package objectstore

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestS3PresignUpload(t *testing.T) {
	b, err := NewS3(Config{
		S3Endpoint:  "https://s3.example.test",
		S3Bucket:    "bucket",
		S3Region:    "us-east-1",
		S3AccessKey: "AKIDEXAMPLE",
		S3SecretKey: "secret",
		S3Prefix:    "app/prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	var backend Backend = b
	p, ok := backend.(Presigner)
	if !ok {
		t.Fatal("S3 must implement Presigner")
	}
	up, err := p.PresignUpload(context.Background(), "files/a.png", "image/png", 1234, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if up.Method != "PUT" || up.ObjectKey != "files/a.png" || up.URL == "" {
		t.Fatalf("unexpected presigned upload: %+v", up)
	}
	if up.ExpiresAt.Before(time.Now()) {
		t.Fatal("presigned upload must not be expired")
	}
	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("X-Amz-Signature") == "" {
		t.Fatalf("missing signature in %s", up.URL)
	}
	t.Logf("signed headers=%q path=%s", q.Get("X-Amz-SignedHeaders"), u.Path)
	if !strings.Contains(u.Path, "app/prod/files/a.png") {
		t.Fatalf("prefix not applied: %s", u.Path)
	}
	if !strings.Contains(q.Get("X-Amz-SignedHeaders"), "content-length") {
		t.Fatalf("size must be signed as content-length: %q", q.Get("X-Amz-SignedHeaders"))
	}
}

func TestLocalBackendHasNoPresigner(t *testing.T) {
	var backend Backend = NewLocal(t.TempDir())
	if _, ok := backend.(Presigner); ok {
		t.Fatal("local backend must not advertise presigned uploads")
	}
}
