package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/query"
)

type discountRequest struct {
	Kind        string `json:"kind"`
	BasisPoints int    `json:"basis_points"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	BuyQuantity int    `json:"buy_quantity"`
	FreeUnits   int    `json:"free_units"`
}

type targetRequest struct {
	SKUs       []string `json:"skus"`
	Sellers    []string `json:"sellers"`
	Categories []string `json:"categories"`
}

type promotionRequest struct {
	Name      string          `json:"name"`
	Discount  discountRequest `json:"discount"`
	Target    targetRequest   `json:"target"`
	Priority  int             `json:"priority"`
	Exclusive bool            `json:"exclusive"`
	StartsAt  *time.Time      `json:"starts_at"`
	EndsAt    *time.Time      `json:"ends_at"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type promoCodeRequest struct {
	Code             string          `json:"code"`
	Discount         discountRequest `json:"discount"`
	MinCartAmount    int64           `json:"min_cart_amount"`
	TotalLimit       int             `json:"total_limit"`
	PerCustomerLimit int             `json:"per_customer_limit"`
	StartsAt         *time.Time      `json:"starts_at"`
	EndsAt           *time.Time      `json:"ends_at"`
}

type quoteLineRequest struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type quoteRequest struct {
	Lines     []quoteLineRequest `json:"lines"`
	PromoCode string             `json:"promo_code"`
}

type compareAtRequest struct {
	CompareAt int64 `json:"compare_at"`
}

type discountResponse struct {
	RuleID    string `json:"rule_id"`
	Kind      string `json:"kind"`
	Amount    int64  `json:"amount"`
	PromoCode string `json:"promo_code,omitempty"`
}

type quoteLineResponse struct {
	SKU       string             `json:"sku"`
	SellerID  string             `json:"seller_id"`
	ProductID string             `json:"product_id"`
	Quantity  int                `json:"quantity"`
	UnitPrice int64              `json:"unit_price"`
	CompareAt int64              `json:"compare_at,omitempty"`
	Base      int64              `json:"base"`
	Discounts []discountResponse `json:"discounts"`
	Final     int64              `json:"final"`
}

type quoteResponse struct {
	Lines     []quoteLineResponse `json:"lines"`
	Subtotal  int64               `json:"subtotal"`
	Discount  int64               `json:"discount"`
	Total     int64               `json:"total"`
	Currency  string              `json:"currency"`
	PromoCode string              `json:"promo_code,omitempty"`
}

type promotionCreatedResponse struct {
	PromotionID string `json:"promotion_id"`
}

type promoCodeCreatedResponse struct {
	Code string `json:"code"`
}

type promotionResponse struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Discount  discountRequest `json:"discount"`
	Target    targetRequest   `json:"target"`
	Priority  int             `json:"priority"`
	Exclusive bool            `json:"exclusive"`
	Status    string          `json:"status"`
	StartsAt  *time.Time      `json:"starts_at,omitempty"`
	EndsAt    *time.Time      `json:"ends_at,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type promoCodeResponse struct {
	Code             string          `json:"code"`
	Discount         discountRequest `json:"discount"`
	MinCartAmount    int64           `json:"min_cart_amount"`
	TotalLimit       int             `json:"total_limit"`
	PerCustomerLimit int             `json:"per_customer_limit"`
	Used             int             `json:"used"`
	Status           string          `json:"status"`
	StartsAt         *time.Time      `json:"starts_at,omitempty"`
	EndsAt           *time.Time      `json:"ends_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

func toDiscountInput(r discountRequest) command.DiscountInput {
	return command.DiscountInput{
		Kind: r.Kind, BasisPoints: r.BasisPoints, Amount: r.Amount, Currency: r.Currency,
		BuyQuantity: r.BuyQuantity, FreeUnits: r.FreeUnits,
	}
}

func toPromotionInput(r promotionRequest) command.PromotionInput {
	return command.PromotionInput{
		Name: r.Name, Discount: toDiscountInput(r.Discount), Priority: r.Priority, Exclusive: r.Exclusive,
		Target:   command.TargetInput{SKUs: r.Target.SKUs, Sellers: r.Target.Sellers, Categories: r.Target.Categories},
		StartsAt: value(r.StartsAt), EndsAt: value(r.EndsAt),
	}
}

func value(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func pointer(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func toQuote(view query.QuoteView) quoteResponse {
	out := quoteResponse{
		Subtotal: view.Subtotal, Discount: view.Discount, Total: view.Total, Currency: view.Currency,
		PromoCode: view.PromoCode, Lines: make([]quoteLineResponse, 0, len(view.Lines)),
	}
	for _, line := range view.Lines {
		lineResponse := quoteLineResponse{
			SKU: line.SKU, SellerID: line.SellerID, ProductID: line.ProductID, Quantity: line.Quantity,
			UnitPrice: line.UnitPrice, CompareAt: line.CompareAt, Base: line.Base, Final: line.Final,
			Discounts: make([]discountResponse, 0, len(line.Discounts)),
		}
		for _, discount := range line.Discounts {
			lineResponse.Discounts = append(lineResponse.Discounts, discountResponse{
				RuleID: discount.RuleID, Kind: discount.Kind, Amount: discount.Amount, PromoCode: discount.PromoCode,
			})
		}
		out.Lines = append(out.Lines, lineResponse)
	}
	return out
}

func toPromotion(view query.PromotionView) promotionResponse {
	return promotionResponse{
		ID: view.ID, Name: view.Name, Priority: view.Priority, Exclusive: view.Exclusive, Status: view.Status,
		Discount: discountRequest{
			Kind: view.Kind, BasisPoints: view.BasisPoints, Amount: view.Amount, Currency: view.Currency,
			BuyQuantity: view.BuyQuantity, FreeUnits: view.FreeUnits,
		},
		Target:   targetRequest{SKUs: view.SKUs, Sellers: view.Sellers, Categories: view.Categories},
		StartsAt: pointer(view.StartsAt), EndsAt: pointer(view.EndsAt), CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
	}
}

func toPromoCode(view query.PromoCodeView) promoCodeResponse {
	return promoCodeResponse{
		Code: view.Code, MinCartAmount: view.MinCartAmount, TotalLimit: view.TotalLimit,
		PerCustomerLimit: view.PerCustomerLimit, Used: view.Used, Status: view.Status,
		Discount: discountRequest{Kind: view.Kind, BasisPoints: view.BasisPoints, Amount: view.Amount, Currency: view.Currency},
		StartsAt: pointer(view.StartsAt), EndsAt: pointer(view.EndsAt), CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
	}
}
