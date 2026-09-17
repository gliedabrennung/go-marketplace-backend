package httpapi

import (
	"errors"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type itemRequest struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type quantityRequest struct {
	Quantity int `json:"quantity"`
}

type promoRequest struct {
	Code string `json:"code"`
}

type previewRequest struct {
	DeliveryMethod string `json:"delivery_method"`
}

type lineResponse struct {
	SKU          string `json:"sku"`
	ProductID    string `json:"product_id,omitempty"`
	Title        string `json:"title,omitempty"`
	CoverKey     string `json:"cover_key,omitempty"`
	Quantity     int    `json:"quantity"`
	Available    int    `json:"available"`
	Status       string `json:"status"`
	UnitPrice    int64  `json:"unit_price"`
	SavedPrice   int64  `json:"saved_price"`
	PriceChanged bool   `json:"price_changed"`
	CompareAt    int64  `json:"compare_at,omitempty"`
	Subtotal     int64  `json:"subtotal"`
	Discount     int64  `json:"discount"`
	Total        int64  `json:"total"`
}

type groupResponse struct {
	SellerID string         `json:"seller_id"`
	Items    []lineResponse `json:"items"`
	Subtotal int64          `json:"subtotal"`
	Discount int64          `json:"discount"`
	Shipping int64          `json:"shipping"`
	Total    int64          `json:"total"`
}

type issueResponse struct {
	SKU  string `json:"sku"`
	Code string `json:"code"`
}

type promoErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type cartResponse struct {
	CartID         string              `json:"cart_id,omitempty"`
	Currency       string              `json:"currency,omitempty"`
	PromoCode      string              `json:"promo_code,omitempty"`
	PromoError     *promoErrorResponse `json:"promo_error,omitempty"`
	DeliveryMethod string              `json:"delivery_method"`
	Groups         []groupResponse     `json:"groups"`
	ItemsCount     int                 `json:"items_count"`
	Subtotal       int64               `json:"subtotal"`
	Discount       int64               `json:"discount"`
	Shipping       int64               `json:"shipping"`
	Total          int64               `json:"total"`
	Issues         []issueResponse     `json:"issues"`
	Ready          bool                `json:"ready"`
	UpdatedAt      *time.Time          `json:"updated_at,omitempty"`
	ExpiresAt      *time.Time          `json:"expires_at,omitempty"`
}

func toCart(view application.View) cartResponse {
	out := cartResponse{
		CartID: view.CartID, Currency: view.Currency, PromoCode: view.PromoCode, DeliveryMethod: view.DeliveryMethod,
		ItemsCount: view.ItemsCount, Subtotal: view.Subtotal, Discount: view.Discount, Shipping: view.Shipping,
		Total: view.Total, Ready: view.Ready, Groups: make([]groupResponse, 0, len(view.Groups)),
		Issues:    make([]issueResponse, 0, len(view.Issues)),
		UpdatedAt: optionalTime(view.UpdatedAt), ExpiresAt: optionalTime(view.ExpiresAt),
	}
	var promoErr *kernel.Error
	if errors.As(view.PromoError, &promoErr) {
		out.PromoError = &promoErrorResponse{Code: promoErr.Code(), Message: promoErr.Message()}
	}
	for _, group := range view.Groups {
		item := groupResponse{
			SellerID: group.SellerID, Subtotal: group.Subtotal, Discount: group.Discount, Shipping: group.Shipping,
			Total: group.Total, Items: make([]lineResponse, 0, len(group.Lines)),
		}
		for _, line := range group.Lines {
			item.Items = append(item.Items, lineResponse{
				SKU: line.SKU, ProductID: line.ProductID, Title: line.Title, CoverKey: line.CoverKey, Quantity: line.Quantity,
				Available: line.Available, Status: string(line.Status), UnitPrice: line.UnitPrice, SavedPrice: line.SavedPrice,
				PriceChanged: line.PriceChanged, CompareAt: line.CompareAt, Subtotal: line.Base, Discount: line.Discount,
				Total: line.Final,
			})
		}
		out.Groups = append(out.Groups, item)
	}
	for _, issue := range view.Issues {
		out.Issues = append(out.Issues, issueResponse(issue))
	}
	return out
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
