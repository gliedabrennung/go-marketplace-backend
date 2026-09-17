package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

var ErrObjectNotFound = errors.New("object not found")

type Config struct {
	Endpoint       string
	PublicEndpoint string
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
}

type PresignedRequest struct {
	Method    string
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

type ObjectInfo struct {
	Size        int64
	ContentType string
}

type Client struct {
	internal *s3.Client
	presign  *s3.PresignClient
	bucket   string
}

func New(cfg Config) *Client {
	publicEndpoint := cfg.PublicEndpoint
	if publicEndpoint == "" {
		publicEndpoint = cfg.Endpoint
	}
	awsCfg := aws.Config{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
	}
	return &Client{
		internal: newS3(awsCfg, cfg.Endpoint),
		presign:  s3.NewPresignClient(newS3(awsCfg, publicEndpoint)),
		bucket:   cfg.Bucket,
	}
}

func newS3(cfg aws.Config, endpoint string) *s3.Client {
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
}

func (c *Client) EnsureBucket(ctx context.Context) error {
	if _, err := c.internal.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)}); err == nil {
		return nil
	}
	_, err := c.internal.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)})
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok && (apiErr.ErrorCode() == "BucketAlreadyOwnedByYou" || apiErr.ErrorCode() == "BucketAlreadyExists") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create bucket %s: %w", c.bucket, err)
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.internal.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)}); err != nil {
		return fmt.Errorf("head bucket %s: %w", c.bucket, err)
	}
	return nil
}

func (c *Client) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (PresignedRequest, error) {
	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign put %s: %w", key, err)
	}
	headers := map[string]string{"Content-Type": contentType}
	for name, values := range req.SignedHeader {
		if len(values) > 0 && name != "Host" {
			headers[http.CanonicalHeaderKey(name)] = values[0]
		}
	}
	return PresignedRequest{Method: req.Method, URL: req.URL, Headers: headers, ExpiresAt: time.Now().Add(ttl)}, nil
}

func (c *Client) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign get %s: %w", key, err)
	}
	return req.URL, nil
}

func (c *Client) Head(ctx context.Context, key string) (ObjectInfo, error) {
	out, err := c.internal.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return ObjectInfo{}, notFoundOr(err, "head %s", key)
	}
	return ObjectInfo{Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType)}, nil
}

func (c *Client) ReadPrefix(ctx context.Context, key string, limit int64) (data []byte, err error) {
	out, err := c.internal.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=0-%d", limit-1)),
	})
	if err != nil {
		return nil, notFoundOr(err, "read %s", key)
	}
	defer func() {
		if closeErr := out.Body.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	data, err = io.ReadAll(io.LimitReader(out.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	return data, nil
}

func (c *Client) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := c.internal.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, notFoundOr(err, "open %s", key)
	}
	return out.Body, nil
}

func (c *Client) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := c.internal.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(int64(len(data))),
	})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

func notFoundOr(err error, format string, args ...any) error {
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "404":
			return ErrObjectNotFound
		}
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}
