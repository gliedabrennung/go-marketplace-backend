package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/objectstore"
)

const (
	uploadTTL   = 15 * time.Minute
	downloadTTL = time.Hour
)

type Storage struct {
	client *objectstore.Client
}

func New(client *objectstore.Client) *Storage {
	return &Storage{client: client}
}

func (s *Storage) PresignUpload(ctx context.Context, key, contentType string, size int64) (application.UploadTarget, error) {
	request, err := s.client.PresignPut(ctx, key, contentType, size, uploadTTL)
	if err != nil {
		return application.UploadTarget{}, err
	}
	return application.UploadTarget{
		Method:    request.Method,
		URL:       request.URL,
		Headers:   request.Headers,
		ExpiresAt: request.ExpiresAt,
	}, nil
}

func (s *Storage) SignDownload(ctx context.Context, key string) (string, error) {
	return s.client.PresignGet(ctx, key, downloadTTL)
}

func (s *Storage) Head(ctx context.Context, key string) (application.ObjectInfo, error) {
	info, err := s.client.Head(ctx, key)
	if err != nil {
		return application.ObjectInfo{}, missing(err)
	}
	return application.ObjectInfo{Size: info.Size, ContentType: info.ContentType}, nil
}

func (s *Storage) ReadPrefix(ctx context.Context, key string, limit int64) ([]byte, error) {
	data, err := s.client.ReadPrefix(ctx, key, limit)
	return data, missing(err)
}

func (s *Storage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	body, err := s.client.Open(ctx, key)
	return body, missing(err)
}

func (s *Storage) Put(ctx context.Context, key, contentType string, data []byte) error {
	return s.client.Put(ctx, key, contentType, data)
}

func missing(err error) error {
	if errors.Is(err, objectstore.ErrObjectNotFound) {
		return application.ErrUploadMissing
	}
	return err
}
