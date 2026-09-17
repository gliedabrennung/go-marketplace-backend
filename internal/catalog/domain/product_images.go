package domain

import (
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

const (
	MaxImages     = 15
	MaxImageBytes = 10 << 20
)

const (
	ImageVariantOriginal = "original"
	ImageVariantSmall    = "small.jpg"
	ImageVariantLarge    = "large.jpg"
)

type ImageStatus string

const (
	ImageAwaitingUpload ImageStatus = "awaiting_upload"
	ImageUploaded       ImageStatus = "uploaded"
	ImageProcessed      ImageStatus = "processed"
)

var allowedImageTypes = []string{"image/jpeg", "image/png", "image/webp"}

type Image struct {
	id          ImageID
	contentType string
	size        int64
	status      ImageStatus
	width       int
	height      int
	createdAt   time.Time
}

func (i Image) ID() ImageID { return i.id }

func (i Image) ContentType() string { return i.contentType }

func (i Image) Size() int64 { return i.size }

func (i Image) Status() ImageStatus { return i.status }

func (i Image) Width() int { return i.width }

func (i Image) Height() int { return i.height }

func ImageObjectKey(productID ProductID, imageID ImageID, variant string) string {
	return "catalog/products/" + productID.String() + "/images/" + imageID.String() + "/" + variant
}

type ImageProbe struct {
	ContentType string
	Size        int64
	Width       int
	Height      int
}

func (p *Product) RequestImageUpload(author kernel.SellerID, imageID ImageID, contentType string, size int64, now time.Time) (Image, error) {
	if err := p.requireEditableBy(author); err != nil {
		return Image{}, err
	}
	if imageID.IsZero() {
		return Image{}, kernel.ErrInvalidID
	}
	if len(p.images) >= MaxImages {
		return Image{}, ErrTooManyImages
	}
	if !slices.Contains(allowedImageTypes, contentType) {
		return Image{}, ErrUnsupportedImageType.WithDetail("%q", contentType)
	}
	if size <= 0 || size > MaxImageBytes {
		return Image{}, ErrInvalidImageSize
	}
	img := Image{id: imageID, contentType: contentType, size: size, status: ImageAwaitingUpload, createdAt: now}
	p.images = append(p.images, img)
	p.updatedAt = now
	return img, nil
}

func (p *Product) ConfirmImageUpload(author kernel.SellerID, imageID ImageID, probe ImageProbe, now time.Time) error {
	if err := p.requireEditableBy(author); err != nil {
		return err
	}
	idx, err := p.imageIndex(imageID)
	if err != nil {
		return err
	}
	img := &p.images[idx]
	if img.status != ImageAwaitingUpload {
		return ErrImageAlreadyUploaded
	}
	if probe.ContentType != img.contentType || probe.Size != img.size || probe.Width <= 0 || probe.Height <= 0 {
		return ErrImageMismatch
	}
	img.status = ImageUploaded
	img.width, img.height = probe.Width, probe.Height
	p.updatedAt = now
	p.events.Record(ProductImageUploaded{
		ProductID:   p.id,
		ImageID:     imageID,
		ObjectKey:   ImageObjectKey(p.id, imageID, ImageVariantOriginal),
		ContentType: img.contentType,
		At:          now,
	})
	return nil
}

func (p *Product) MarkImageProcessed(imageID ImageID, now time.Time) error {
	idx, err := p.imageIndex(imageID)
	if err != nil {
		return err
	}
	img := &p.images[idx]
	switch img.status {
	case ImageProcessed:
		return nil
	case ImageAwaitingUpload:
		return ErrImageNotUploaded
	}
	img.status = ImageProcessed
	p.updatedAt = now
	p.events.Record(ProductImageProcessed{
		ProductID: p.id,
		ImageID:   imageID,
		CoverKey:  p.CoverKey(),
		Published: p.status == ProductStatusPublished,
		At:        now,
	})
	return nil
}

func (p *Product) RemoveImage(author kernel.SellerID, imageID ImageID, now time.Time) error {
	if err := p.requireEditableBy(author); err != nil {
		return err
	}
	idx, err := p.imageIndex(imageID)
	if err != nil {
		return err
	}
	p.images = slices.Delete(p.images, idx, idx+1)
	p.updatedAt = now
	return nil
}

func (p *Product) ReorderImages(author kernel.SellerID, order []ImageID, now time.Time) error {
	if err := p.requireEditableBy(author); err != nil {
		return err
	}
	if len(order) != len(p.images) {
		return ErrInvalidImageOrder
	}
	reordered := make([]Image, 0, len(order))
	for _, id := range order {
		idx, err := p.imageIndex(id)
		if err != nil || slices.ContainsFunc(reordered, func(i Image) bool { return i.id == id }) {
			return ErrInvalidImageOrder
		}
		reordered = append(reordered, p.images[idx])
	}
	p.images = reordered
	p.updatedAt = now
	return nil
}

func (p *Product) CoverKey() string {
	for _, img := range p.images {
		if img.status == ImageProcessed {
			return ImageObjectKey(p.id, img.id, ImageVariantSmall)
		}
	}
	return ""
}

func (p *Product) Images() []Image { return slices.Clone(p.images) }

func (p *Product) Image(imageID ImageID) (Image, error) {
	idx, err := p.imageIndex(imageID)
	if err != nil {
		return Image{}, err
	}
	return p.images[idx], nil
}

func (p *Product) imageIndex(imageID ImageID) (int, error) {
	idx := slices.IndexFunc(p.images, func(i Image) bool { return i.id == imageID })
	if idx < 0 {
		return -1, ErrImageNotFound
	}
	return idx, nil
}
