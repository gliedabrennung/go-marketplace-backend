package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
)

const (
	maxPixels        = 50_000_000
	thumbnailQuality = 85
)

var allowedTypes = map[string]struct{}{"image/jpeg": {}, "image/png": {}, "image/webp": {}}

type Prober struct{}

func NewProber() Prober {
	return Prober{}
}

func (Prober) Probe(header []byte) (application.ImageInfo, error) {
	contentType := http.DetectContentType(header)
	if _, ok := allowedTypes[contentType]; !ok {
		return application.ImageInfo{}, application.ErrInvalidImage
	}
	cfg, err := decodeConfig(header)
	if err != nil {
		return application.ImageInfo{}, err
	}
	return application.ImageInfo{ContentType: contentType, Width: cfg.Width, Height: cfg.Height}, nil
}

func decodeConfig(data []byte) (image.Config, error) {
	var (
		cfg    image.Config
		err    error
		reader = bytes.NewReader(data)
	)
	switch http.DetectContentType(data) {
	case "image/jpeg":
		cfg, err = jpeg.DecodeConfig(reader)
	case "image/png":
		cfg, err = png.DecodeConfig(reader)
	case "image/webp":
		cfg, err = webp.DecodeConfig(reader)
	default:
		return image.Config{}, application.ErrInvalidImage
	}
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return image.Config{}, application.ErrInvalidImage
	}
	return cfg, nil
}

func decodeImage(data []byte) (image.Image, error) {
	reader := bytes.NewReader(data)
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return jpeg.Decode(reader)
	case "image/png":
		return png.Decode(reader)
	case "image/webp":
		return webp.Decode(reader)
	default:
		return nil, application.ErrInvalidImage
	}
}

type Objects interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key, contentType string, data []byte) error
}

type Thumbnailer struct {
	objects Objects
}

func NewThumbnailer(objects Objects) *Thumbnailer {
	return &Thumbnailer{objects: objects}
}

func (t *Thumbnailer) Render(ctx context.Context, sourceKey string, targets []application.Thumbnail) error {
	data, err := t.read(ctx, sourceKey)
	if err != nil {
		return err
	}
	if _, err := decodeConfig(data); err != nil {
		return err
	}
	src, err := decodeImage(data)
	if err != nil {
		return application.ErrInvalidImage
	}
	for _, target := range targets {
		encoded, err := scale(src, target.MaxSide)
		if err != nil {
			return fmt.Errorf("render %s: %w", target.Key, err)
		}
		if err := t.objects.Put(ctx, target.Key, "image/jpeg", encoded); err != nil {
			return err
		}
	}
	return nil
}

func (t *Thumbnailer) read(ctx context.Context, key string) (data []byte, err error) {
	body, err := t.objects.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, body.Close()) }()
	data, err = io.ReadAll(io.LimitReader(body, domain.MaxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	if len(data) > domain.MaxImageBytes {
		return nil, application.ErrInvalidImage
	}
	return data, nil
}

func scale(src image.Image, maxSide int) ([]byte, error) {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if longest := max(width, height); longest > maxSide {
		width = max(1, width*maxSide/longest)
		height = max(1, height*maxSide/longest)
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
