package application

import (
	"context"
	"slices"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/cart/domain"
	pricingapi "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	shippingapi "github.com/gliedabrennung/go-marketplace-backend/internal/shipping/api"
)

type LineStatus string

const (
	LineAvailable    LineStatus = "available"
	LineInsufficient LineStatus = "insufficient_stock"
	LineUnavailable  LineStatus = "unavailable"
)

type Line struct {
	SKU          string
	ProductID    string
	SellerID     string
	Title        string
	CoverKey     string
	Quantity     int
	Available    int
	Status       LineStatus
	SavedPrice   int64
	UnitPrice    int64
	CompareAt    int64
	Base         int64
	Discount     int64
	Final        int64
	PriceChanged bool
}

type Group struct {
	SellerID string
	Lines    []Line
	Subtotal int64
	Discount int64
	Shipping int64
	Total    int64
}

type Issue struct {
	SKU  string
	Code string
}

type View struct {
	CartID         string
	Currency       string
	PromoCode      string
	PromoError     error
	DeliveryMethod string
	Groups         []Group
	ItemsCount     int
	Subtotal       int64
	Discount       int64
	Shipping       int64
	Total          int64
	Issues         []Issue
	Ready          bool
	UpdatedAt      time.Time
	ExpiresAt      time.Time
}

type Assembler struct {
	offers  Offers
	stock   Stock
	pricing Pricing
	tariffs Tariffs
}

func NewAssembler(offers Offers, stock Stock, pricing Pricing, tariffs Tariffs) *Assembler {
	return &Assembler{offers: offers, stock: stock, pricing: pricing, tariffs: tariffs}
}

func (a *Assembler) Assemble(ctx context.Context, cart *domain.Cart, customerID, method string) (View, error) {
	if method == "" {
		method = shippingapi.MethodStandard
	}
	view := View{
		Currency: cart.Currency(), PromoCode: cart.PromoCode(), DeliveryMethod: method, Groups: []Group{}, Issues: []Issue{},
		UpdatedAt: cart.UpdatedAt(), ExpiresAt: cart.ExpiresAt(),
	}
	if !cart.ID().IsZero() {
		view.CartID = cart.ID().String()
	}
	if cart.IsEmpty() {
		return view, nil
	}
	lines, err := a.lines(ctx, cart)
	if err != nil {
		return View{}, err
	}
	quote, err := a.quote(ctx, lines, cart.PromoCode(), customerID, &view)
	if err != nil {
		return View{}, err
	}
	apply(lines, quote)
	if err := a.group(ctx, &view, lines, quote); err != nil {
		return View{}, err
	}
	view.Ready = len(view.Issues) == 0 && view.PromoError == nil && view.Total > 0
	return view, nil
}

func (a *Assembler) lines(ctx context.Context, cart *domain.Cart) ([]Line, error) {
	items := cart.Items()
	skus := make([]string, 0, len(items))
	for _, item := range items {
		skus = append(skus, item.SKU())
	}
	offers, err := a.offers.Offers(ctx, skus)
	if err != nil {
		return nil, err
	}
	stock, err := a.stock.Available(ctx, skus)
	if err != nil {
		return nil, err
	}
	lines := make([]Line, 0, len(items))
	for _, item := range items {
		offer, found := offers[item.SKU()]
		line := Line{
			SKU: item.SKU(), ProductID: offer.ProductID, SellerID: item.SellerID(), Title: offer.Title, CoverKey: offer.CoverKey,
			Quantity: item.Quantity(), Available: stock[item.SKU()], SavedPrice: item.Price(), UnitPrice: offer.PriceAmount,
		}
		switch {
		case !Purchasable(offer, found) || line.Available <= 0:
			line.Status, line.Available = LineUnavailable, max(line.Available, 0)
		case line.Available < line.Quantity:
			line.Status = LineInsufficient
		default:
			line.Status = LineAvailable
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func (a *Assembler) quote(ctx context.Context, lines []Line, promo, customerID string, view *View) (pricingapi.Quote, error) {
	for attempt := 0; ; attempt++ {
		request := pricingapi.QuoteRequest{PromoCode: promo, CustomerID: customerID}
		for _, line := range lines {
			if line.Status == LineAvailable {
				request.Lines = append(request.Lines, pricingapi.QuoteLine{SKU: line.SKU, Quantity: line.Quantity})
			}
		}
		if len(request.Lines) == 0 {
			return pricingapi.Quote{}, nil
		}
		quote, err := a.pricing.Quote(ctx, request)
		if promo != "" && pricingapi.IsPromoCodeError(err) {
			view.PromoError = err
			request.PromoCode = ""
			quote, err = a.pricing.Quote(ctx, request)
		}
		if !pricingapi.IsUnpricedOffer(err) || attempt > 0 {
			return quote, err
		}
		if err := a.exclude(ctx, lines, customerID); err != nil {
			return pricingapi.Quote{}, err
		}
	}
}

func (a *Assembler) exclude(ctx context.Context, lines []Line, customerID string) error {
	for i := range lines {
		if lines[i].Status != LineAvailable {
			continue
		}
		_, err := a.pricing.Quote(ctx, pricingapi.QuoteRequest{
			CustomerID: customerID, Lines: []pricingapi.QuoteLine{{SKU: lines[i].SKU, Quantity: lines[i].Quantity}},
		})
		if pricingapi.IsUnpricedOffer(err) {
			lines[i].Status = LineUnavailable
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func apply(lines []Line, quote pricingapi.Quote) {
	for i := range lines {
		index := slices.IndexFunc(quote.Lines, func(priced pricingapi.PricedLine) bool { return priced.SKU == lines[i].SKU })
		if index < 0 {
			continue
		}
		priced := quote.Lines[index]
		lines[i].UnitPrice, lines[i].CompareAt, lines[i].Base, lines[i].Final = priced.UnitPrice, priced.CompareAt, priced.Base, priced.Final
		lines[i].Discount = priced.Base - priced.Final
		lines[i].PriceChanged = lines[i].SavedPrice != priced.UnitPrice
	}
}

func (a *Assembler) group(ctx context.Context, view *View, lines []Line, quote pricingapi.Quote) error {
	parcels := []shippingapi.Parcel{}
	for _, line := range lines {
		view.ItemsCount += line.Quantity
		switch line.Status {
		case LineUnavailable:
			view.Issues = append(view.Issues, Issue{SKU: line.SKU, Code: domain.ErrOfferUnavailable.Code()})
		case LineInsufficient:
			view.Issues = append(view.Issues, Issue{SKU: line.SKU, Code: domain.ErrExceedsStock.Code()})
		}
		index := slices.IndexFunc(view.Groups, func(g Group) bool { return g.SellerID == line.SellerID })
		if index < 0 {
			view.Groups = append(view.Groups, Group{SellerID: line.SellerID})
			index = len(view.Groups) - 1
		}
		group := &view.Groups[index]
		group.Lines = append(group.Lines, line)
		if line.Status == LineAvailable {
			group.Subtotal += line.Base
			group.Discount += line.Discount
			group.Total += line.Final
		}
	}
	for _, group := range view.Groups {
		if group.Total > 0 {
			parcels = append(parcels, shippingapi.Parcel{SellerID: group.SellerID, Subtotal: group.Total})
		}
	}
	if len(parcels) == 0 {
		return nil
	}
	tariff, err := a.tariffs.Quote(ctx, shippingapi.TariffRequest{Method: view.DeliveryMethod, Currency: quote.Currency, Parcels: parcels})
	if err != nil {
		return err
	}
	for _, cost := range tariff.Parcels {
		index := slices.IndexFunc(view.Groups, func(g Group) bool { return g.SellerID == cost.SellerID })
		view.Groups[index].Shipping = cost.Cost
		view.Groups[index].Total += cost.Cost
	}
	view.Currency = quote.Currency
	view.Subtotal, view.Discount, view.Shipping = quote.Subtotal, quote.Discount, tariff.Total
	view.Total = quote.Total + tariff.Total
	return nil
}
