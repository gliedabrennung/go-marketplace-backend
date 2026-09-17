package command_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	cartapi "github.com/gliedabrennung/go-marketplace-backend/internal/cart/api"
	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	inventoryapi "github.com/gliedabrennung/go-marketplace-backend/internal/inventory/api"
	paymentapi "github.com/gliedabrennung/go-marketplace-backend/internal/payment/api"
	pricingapi "github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var (
	errTransient = errors.New("connection reset")
	errRejected  = kernel.BusinessRule("TEST_REJECTED", "rejected")
)

type carts struct {
	mu       sync.Mutex
	checkout map[string]cartapi.Checkout
	removed  map[string][]string
}

func (c *carts) Checkout(_ context.Context, userID string) (cartapi.Checkout, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	checkout, ok := c.checkout[userID]
	if !ok {
		return cartapi.Checkout{}, cartapi.ErrCartEmpty
	}
	return checkout, nil
}

func (c *carts) RemoveOrdered(_ context.Context, userID string, skus []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removed[userID] = append(c.removed[userID], skus...)
	return nil
}

type catalog map[string]catalogapi.OfferSummary

func (c catalog) Offers(_ context.Context, ids []string) (map[string]catalogapi.OfferSummary, error) {
	out := map[string]catalogapi.OfferSummary{}
	for _, id := range ids {
		if offer, ok := c[id]; ok {
			out[id] = offer
		}
	}
	return out, nil
}

type pricing struct {
	mu          sync.Mutex
	prices      map[string]pricingapi.PricedLine
	redeemed    map[string]bool
	failRedeem  error
	failRelease error
}

func (p *pricing) Quote(_ context.Context, request pricingapi.QuoteRequest) (pricingapi.Quote, error) {
	quote := pricingapi.Quote{Currency: "KZT"}
	for _, line := range request.Lines {
		price, ok := p.prices[line.SKU]
		if !ok {
			return pricingapi.Quote{}, pricingapi.ErrOfferPriceNotFound
		}
		base := price.UnitPrice * int64(line.Quantity)
		quote.Lines = append(quote.Lines, pricingapi.PricedLine{
			SKU: line.SKU, SellerID: price.SellerID, ProductID: price.ProductID, Quantity: line.Quantity,
			UnitPrice: price.UnitPrice, Base: base, Final: base,
		})
		quote.Subtotal += base
	}
	quote.Total = quote.Subtotal
	if request.PromoCode == "SALE10" {
		discount := quote.Lines[0].Final / 10
		quote.Lines[0].Final -= discount
		quote.Lines[0].Discounts = []pricingapi.Discount{{Kind: "promo_code", Amount: discount, PromoCode: "SALE10"}}
		quote.Discount, quote.Total, quote.PromoCode = discount, quote.Total-discount, "SALE10"
	}
	return quote, nil
}

func (p *pricing) Redeem(_ context.Context, code, orderID, _ string, _ int64, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failRedeem != nil {
		return p.failRedeem
	}
	p.redeemed[code+"/"+orderID] = true
	return nil
}

func (p *pricing) Release(_ context.Context, code, orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failRelease != nil {
		return p.failRelease
	}
	delete(p.redeemed, code+"/"+orderID)
	return nil
}

type inventory struct {
	mu           sync.Mutex
	stock        map[string]int
	reservations map[string]string
	lines        map[string][]inventoryapi.ReserveLine
	ttl          time.Duration
	now          func() time.Time
	failCommit   error
	failRelease  error
}

func (i *inventory) Reserve(_ context.Context, id, _ string, lines []inventoryapi.ReserveLine) (inventoryapi.Reservation, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, line := range lines {
		if i.stock[line.SKU] < line.Quantity {
			return inventoryapi.Reservation{}, inventoryapi.ErrInsufficientStock
		}
	}
	for _, line := range lines {
		i.stock[line.SKU] -= line.Quantity
	}
	i.reservations[id], i.lines[id] = "held", lines
	return inventoryapi.Reservation{ReservationID: id, ExpiresAt: i.now().Add(i.ttl)}, nil
}

func (i *inventory) Commit(_ context.Context, id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.failCommit != nil {
		return i.failCommit
	}
	switch i.reservations[id] {
	case "held":
		i.reservations[id] = "committed"
	case "committed":
	case "":
		return inventoryapi.ErrReservationNotFound
	default:
		return inventoryapi.ErrReservationResolved
	}
	return nil
}

func (i *inventory) Release(_ context.Context, id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.failRelease != nil {
		return i.failRelease
	}
	switch i.reservations[id] {
	case "held":
		i.reservations[id] = "released"
		i.giveBack(id)
	case "released":
	case "":
		return inventoryapi.ErrReservationNotFound
	default:
		return inventoryapi.ErrReservationResolved
	}
	return nil
}

func (i *inventory) Restore(_ context.Context, id string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	switch i.reservations[id] {
	case "committed":
		i.reservations[id] = "restored"
		i.giveBack(id)
	case "restored":
	case "":
		return inventoryapi.ErrReservationNotFound
	default:
		return inventoryapi.ErrReservationNotCommitted
	}
	return nil
}

func (i *inventory) giveBack(id string) {
	for _, line := range i.lines[id] {
		i.stock[line.SKU] += line.Quantity
	}
}

func (i *inventory) status(id string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.reservations[id]
}

type payments struct {
	mu          sync.Mutex
	infos       map[string]paymentapi.Info
	refunds     map[string]string
	failCreate  error
	failCapture error
	failCancel  error
}

func (p *payments) Create(_ context.Context, request paymentapi.CreateRequest) (paymentapi.Created, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failCreate != nil {
		return paymentapi.Created{}, p.failCreate
	}
	if existing, ok := p.infos[request.PaymentID]; ok {
		return paymentapi.Created{PaymentID: existing.PaymentID, RedirectURL: existing.RedirectURL, Status: existing.Status}, nil
	}
	info := paymentapi.Info{
		PaymentID: request.PaymentID, OrderID: request.OrderID, Status: paymentapi.StatusPending, Amount: request.Amount,
		Currency: request.Currency, RedirectURL: "https://psp.example/pay/" + request.PaymentID + "?return=" + request.ReturnURL,
	}
	p.infos[request.PaymentID] = info
	return paymentapi.Created{PaymentID: info.PaymentID, RedirectURL: info.RedirectURL, Status: info.Status}, nil
}

func (p *payments) set(id string, change func(info *paymentapi.Info)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	info := p.infos[id]
	change(&info)
	p.infos[id] = info
}

func (p *payments) Capture(_ context.Context, id string, amount int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failCapture != nil {
		return p.failCapture
	}
	info, ok := p.infos[id]
	if !ok {
		return paymentapi.ErrPaymentNotFound
	}
	switch info.Status {
	case paymentapi.StatusCaptured:
		return nil
	case paymentapi.StatusAuthorized:
		info.Status, info.Captured = paymentapi.StatusCaptured, amount
		p.infos[id] = info
		return nil
	}
	return paymentapi.ErrInvalidTransition
}

func (p *payments) Cancel(_ context.Context, id, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failCancel != nil {
		return p.failCancel
	}
	info, ok := p.infos[id]
	if !ok {
		return paymentapi.ErrPaymentNotFound
	}
	switch info.Status {
	case paymentapi.StatusCancelled, paymentapi.StatusFailed:
		return nil
	case paymentapi.StatusCaptured, paymentapi.StatusRefunded:
		return paymentapi.ErrInvalidTransition
	}
	info.Status = paymentapi.StatusCancelled
	p.infos[id] = info
	return nil
}

func (p *payments) Refund(_ context.Context, id, refundID string, _ int64, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	info, ok := p.infos[id]
	if !ok {
		return paymentapi.ErrPaymentNotFound
	}
	if info.Status != paymentapi.StatusCaptured {
		return paymentapi.ErrInvalidTransition
	}
	info.Status, info.Refunded = paymentapi.StatusRefunded, info.Captured
	p.infos[id] = info
	p.refunds[id] = refundID
	return nil
}

func (p *payments) Info(_ context.Context, id string) (paymentapi.Info, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	info, ok := p.infos[id]
	if !ok {
		return paymentapi.Info{}, paymentapi.ErrPaymentNotFound
	}
	return info, nil
}

func (p *payments) status(id string) string {
	info, _ := p.Info(context.Background(), id)
	return info.Status
}

type metrics struct {
	mu     sync.Mutex
	counts map[string]int
}

func (m *metrics) inc(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[key]++
}

func (m *metrics) OrderPlaced(status, currency string, _ int64) {
	m.inc("placed:" + status + ":" + currency)
}

func (m *metrics) SagaStep(step, outcome string) { m.inc(fmt.Sprintf("step:%s:%s", step, outcome)) }

func (m *metrics) Compensation(step, outcome string) {
	m.inc(fmt.Sprintf("compensation:%s:%s", step, outcome))
}

func (m *metrics) get(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[key]
}

type membership map[string]string

func (m membership) MemberRole(_ context.Context, sellerID, userID string) (string, bool, error) {
	return "seller_admin", m[sellerID] == userID, nil
}
