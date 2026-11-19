package objectstore

import (
	"context"
	"testing"
	"time"
)

func TestPublicURLFromConfig(t *testing.T) {
	local, err := Open(Config{Driver: "local", LocalDir: t.TempDir(), PublicURL: "/uploads/"})
	if err != nil {
		t.Fatal(err)
	}
	lp, ok := local.(PublicURLer)
	if !ok {
		t.Fatal("local must implement PublicURLer")
	}
	if got := lp.PublicURL("a/b.png"); got != "/uploads/a/b.png" {
		t.Fatalf("local public URL = %q", got)
	}

	s3, err := Open(Config{
		Driver:      "s3",
		S3Endpoint:  "https://r2.example.test",
		S3Bucket:    "bucket",
		S3AccessKey: "AKIDEXAMPLE",
		S3SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	sp := s3.(PublicURLer)
	if got := sp.PublicURL("a/b.png"); got != "https://r2.example.test/bucket/a/b.png" {
		t.Fatalf("path-style public URL = %q", got)
	}

	aws, err := Open(Config{
		Driver:      "s3",
		S3Endpoint:  "https://s3.us-east-1.amazonaws.com",
		S3Bucket:    "bucket",
		S3AccessKey: "AKIDEXAMPLE",
		S3SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	ap := aws.(PublicURLer)
	if got := ap.PublicURL("a/b.png"); got != "https://bucket.s3.us-east-1.amazonaws.com/a/b.png" {
		t.Fatalf("virtual-host public URL = %q", got)
	}

	cdn, err := Open(Config{
		Driver:          "s3",
		S3Endpoint:      "https://r2.example.test",
		S3Bucket:        "bucket",
		S3PublicBaseURL: "https://cdn.example.com/",
		S3AccessKey:     "AKIDEXAMPLE",
		S3SecretKey:     "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cdn.(PublicURLer).PublicURL("a/b.png"); got != "https://cdn.example.com/a/b.png" {
		t.Fatalf("CDN public URL = %q", got)
	}
}

func TestS3PresignTTLFromConfig(t *testing.T) {
	b, err := NewS3(Config{
		S3Endpoint:   "https://s3.example.test",
		S3Bucket:     "bucket",
		S3AccessKey:  "AKIDEXAMPLE",
		S3SecretKey:  "secret",
		S3PresignTTL: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	up, err := b.PresignUpload(context.Background(), "k.txt", "text/plain", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().Add(30 * time.Second)
	if up.ExpiresAt.Before(want.Add(-time.Second)) || up.ExpiresAt.After(want.Add(time.Second)) {
		t.Fatalf("expiry = %v, want about %v", up.ExpiresAt, want)
	}
}
