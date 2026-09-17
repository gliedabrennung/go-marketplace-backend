package memory

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
)

const uploadTTL = 15 * time.Minute

type object struct {
	contentType string
	data        []byte
}

type Objects struct {
	mu      sync.Mutex
	objects map[string]object
}

func NewObjects() *Objects {
	return &Objects{objects: make(map[string]object)}
}

func (o *Objects) PresignUpload(_ context.Context, key, contentType string, size int64) (application.UploadTarget, error) {
	return application.UploadTarget{
		Method:    http.MethodPut,
		URL:       "memory://" + key,
		Headers:   map[string]string{"Content-Type": contentType, "Content-Length": strconv.FormatInt(size, 10)},
		ExpiresAt: time.Now().UTC().Add(uploadTTL),
	}, nil
}

func (o *Objects) SignDownload(_ context.Context, key string) (string, error) {
	return "memory://" + key, nil
}

func (o *Objects) Head(_ context.Context, key string) (application.ObjectInfo, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	obj, ok := o.objects[key]
	if !ok {
		return application.ObjectInfo{}, application.ErrUploadMissing
	}
	return application.ObjectInfo{Size: int64(len(obj.data)), ContentType: obj.contentType}, nil
}

func (o *Objects) ReadPrefix(_ context.Context, key string, limit int64) ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	obj, ok := o.objects[key]
	if !ok {
		return nil, application.ErrUploadMissing
	}
	return slices.Clone(obj.data[:min(int64(len(obj.data)), limit)]), nil
}

func (o *Objects) Open(_ context.Context, key string) (io.ReadCloser, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	obj, ok := o.objects[key]
	if !ok {
		return nil, application.ErrUploadMissing
	}
	return io.NopCloser(bytes.NewReader(slices.Clone(obj.data))), nil
}

func (o *Objects) Put(_ context.Context, key, contentType string, data []byte) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.objects[key] = object{contentType: contentType, data: slices.Clone(data)}
	return nil
}

func (o *Objects) Get(key string) ([]byte, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	obj, ok := o.objects[key]
	return slices.Clone(obj.data), ok
}
