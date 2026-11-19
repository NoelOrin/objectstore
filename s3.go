package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3 stores objects in any S3-compatible service (AWS, R2, MinIO, RustFS).
// Non-AWS endpoints default to path-style addressing, which is what those
// services expect.
type S3 struct {
	client  *s3.Client
	bucket  string
	prefix  string
	timeout time.Duration
}

var _ Backend = (*S3)(nil)

func NewS3(cfg Config) (*S3, error) {
	if strings.TrimSpace(cfg.S3Endpoint) == "" {
		return nil, errors.New("objectstore: s3 endpoint is required")
	}
	if strings.TrimSpace(cfg.S3Bucket) == "" {
		return nil, errors.New("objectstore: s3 bucket is required")
	}
	region := cfg.S3Region
	if region == "" {
		region = "us-east-1"
	}
	endpointURL, err := url.Parse(cfg.S3Endpoint)
	if err != nil {
		return nil, fmt.Errorf("objectstore: invalid s3 endpoint: %w", err)
	}
	isPathStyle := cfg.S3ForcePathStyle || strings.Contains(endpointURL.Host, "localhost") ||
		strings.Contains(endpointURL.Host, "127.0.0.1") || !strings.HasSuffix(endpointURL.Host, "amazonaws.com")

	resolver := aws.EndpointResolverWithOptionsFunc(func(_, _ string, _ ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               strings.TrimRight(cfg.S3Endpoint, "/"),
			SigningRegion:     region,
			HostnameImmutable: !isPathStyle,
		}, nil
	})
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")),
		config.WithEndpointResolverWithOptions(resolver),
	)
	if err != nil {
		return nil, fmt.Errorf("objectstore: load aws config: %w", err)
	}
	var opts []func(*s3.Options)
	if isPathStyle {
		opts = append(opts, func(o *s3.Options) { o.UsePathStyle = true })
	}
	return &S3{
		client:  s3.NewFromConfig(awsCfg, opts...),
		bucket:  cfg.S3Bucket,
		prefix:  strings.Trim(strings.ReplaceAll(cfg.S3Prefix, "\\", "/"), "/"),
		timeout: 10 * time.Second,
	}, nil
}

func (s *S3) Name() string { return "s3" }

func (s *S3) objectKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func (s *S3) ctx(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, s.timeout)
}

func (s *S3) Put(ctx context.Context, key string, data []byte, contentType string) error {
	c, cancel := s.ctx(ctx)
	defer cancel()
	input := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.objectKey(key)),
		Body:          bytes.NewReader(data),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(int64(len(data))),
	}
	if _, err := s.client.PutObject(c, input); err != nil {
		return fmt.Errorf("objectstore: put %s: %w", key, err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) ([]byte, string, error) {
	c, cancel := s.ctx(ctx)
	defer cancel()
	out, err := s.client.GetObject(c, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(key)),
	})
	if err != nil {
		var nf *types.NoSuchKey
		if errors.As(err, &nf) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("objectstore: get %s: %w", key, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(io.LimitReader(out.Body, 64<<20))
	if err != nil {
		return nil, "", err
	}
	mime := ""
	if out.ContentType != nil {
		mime = *out.ContentType
	}
	return data, mime, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	c, cancel := s.ctx(ctx)
	defer cancel()
	_, err := s.client.DeleteObject(c, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(key)),
	})
	if err != nil {
		return fmt.Errorf("objectstore: delete %s: %w", key, err)
	}
	return nil
}

func (s *S3) Stat(ctx context.Context, key string) (int64, int64, error) {
	c, cancel := s.ctx(ctx)
	defer cancel()
	out, err := s.client.HeadObject(c, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(key)),
	})
	if err != nil {
		var nf *types.NotFound
		if errors.As(err, &nf) {
			return 0, 0, ErrNotFound
		}
		return 0, 0, fmt.Errorf("objectstore: stat %s: %w", key, err)
	}
	var size, updated int64
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	if out.LastModified != nil {
		updated = out.LastModified.UnixMilli()
	}
	return size, updated, nil
}

func (s *S3) List(ctx context.Context, prefix string) ([]string, error) {
	c, cancel := s.ctx(ctx)
	defer cancel()
	fullPrefix := s.objectKey(prefix)
	var keys []string
	var token *string
	for {
		out, err := s.client.ListObjectsV2(c, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(fullPrefix),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("objectstore: list %s: %w", prefix, err)
		}
		for _, obj := range out.Contents {
			if obj.Key == nil {
				continue
			}
			key := strings.TrimPrefix(*obj.Key, s.prefix+"/")
			if s.prefix == "" {
				key = *obj.Key
			}
			keys = append(keys, key)
		}
		if out.IsTruncated == nil || !*out.IsTruncated || out.NextContinuationToken == nil {
			break
		}
		token = out.NextContinuationToken
	}
	return keys, nil
}

func (s *S3) TestConnection(ctx context.Context) error {
	c, cancel := s.ctx(ctx)
	defer cancel()
	if _, err := s.client.HeadBucket(c, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)}); err != nil {
		return fmt.Errorf("objectstore: s3 connection test failed: %w", err)
	}
	return nil
}
