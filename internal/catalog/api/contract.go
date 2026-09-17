package api

import (
	"context"
	"time"
)

type ProductDocument struct {
	ProductID    string
	SellerID     string
	CategoryID   string
	CategoryPath []string
	Title        string
	Description  string
	Brand        string
	CoverKey     string
	Attributes   []ProductAttributeV1
	PublishedAt  time.Time
	UpdatedAt    time.Time
}

type FeedCursor struct {
	Since   time.Time
	AfterID string
}

type Feed interface {
	ScanPublished(ctx context.Context, cursor FeedCursor, limit int) ([]ProductDocument, error)
}

type OfferSummary struct {
	OfferID          string
	ProductID        string
	SellerID         string
	SellerSKU        string
	Title            string
	CoverKey         string
	Status           string
	PriceAmount      int64
	Currency         string
	ProductPublished bool
	CategoryID       string
}

type OfferLookup interface {
	Offers(ctx context.Context, offerIDs []string) (map[string]OfferSummary, error)
}
