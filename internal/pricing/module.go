package pricing

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/infrastructure/httpapi"
	"github.com/gliedabrennung/go-marketplace-backend/internal/pricing/infrastructure/postgres"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/cqrs"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

const moduleName = "pricing"

type Dependencies struct {
	Pool        *pgxpool.Pool
	Clock       application.Clock
	Policy      application.Policy
	Sellers     command.SellerMembership
	Responder   *httpx.Responder
	Idempotency httpx.Middleware
	Logger      *slog.Logger
	Metrics     cqrs.Metrics
}

type Module struct {
	api    *httpapi.API
	pricer *pricer
}

func NewModule(d Dependencies) *Module {
	base := command.NewBase(postgres.NewUnitOfWork(d.Pool, postgres.NewOutboxWriter()), d.Clock, d.Policy)
	reads := postgres.NewReadModel(d.Pool)
	quote := decorate(d, query.NewQuoteHandler(reads, d.Clock, d.Policy))

	handlers := httpapi.Handlers{
		CreatePromotion:    decorate(d, command.NewCreatePromotionHandler(base)),
		UpdatePromotion:    decorate(d, command.NewUpdatePromotionHandler(base)),
		SetPromotionStatus: decorate(d, command.NewSetPromotionStatusHandler(base)),
		CreatePromoCode:    decorate(d, command.NewCreatePromoCodeHandler(base)),
		SetPromoCodeStatus: decorate(d, command.NewSetPromoCodeStatusHandler(base)),
		SetCompareAt:       decorate(d, command.NewSetCompareAtPriceHandler(base, d.Sellers)),
		Quote:              quote,
		GetPromotion:       decorate(d, query.NewGetPromotionHandler(reads)),
		ListPromotions:     decorate(d, query.NewListPromotionsHandler(reads)),
		GetPromoCode:       decorate(d, query.NewGetPromoCodeHandler(reads)),
	}

	return &Module{
		api: httpapi.NewAPI(handlers, d.Responder, d.Idempotency),
		pricer: &pricer{
			quote:   quote,
			redeem:  decorate(d, command.NewRedeemPromoCodeHandler(base)),
			release: decorate(d, command.NewReleasePromoCodeHandler(base)),
		},
	}
}

func (m *Module) RegisterRoutes(rt *httpx.Router) {
	m.api.Register(rt)
}

func (m *Module) Pricer() api.Pricer { return m.pricer }

type pricer struct {
	quote   cqrs.Handler[query.Quote, query.QuoteView]
	redeem  cqrs.Handler[command.RedeemPromoCode, struct{}]
	release cqrs.Handler[command.ReleasePromoCode, struct{}]
}

func (p *pricer) Quote(ctx context.Context, request api.QuoteRequest) (api.Quote, error) {
	q := query.Quote{PromoCode: request.PromoCode, CustomerID: request.CustomerID, Lines: make([]query.QuoteLine, 0, len(request.Lines))}
	for _, line := range request.Lines {
		q.Lines = append(q.Lines, query.QuoteLine{SKU: line.SKU, Quantity: line.Quantity})
	}
	view, err := p.quote.Handle(ctx, q)
	if err != nil {
		return api.Quote{}, err
	}
	out := api.Quote{
		Subtotal: view.Subtotal, Discount: view.Discount, Total: view.Total, Currency: view.Currency,
		PromoCode: view.PromoCode, Lines: make([]api.PricedLine, 0, len(view.Lines)),
	}
	for _, line := range view.Lines {
		priced := api.PricedLine{
			SKU: line.SKU, SellerID: line.SellerID, ProductID: line.ProductID, Quantity: line.Quantity,
			UnitPrice: line.UnitPrice, CompareAt: line.CompareAt, Base: line.Base, Final: line.Final,
			Discounts: make([]api.Discount, 0, len(line.Discounts)),
		}
		for _, discount := range line.Discounts {
			priced.Discounts = append(priced.Discounts, api.Discount{
				RuleID: discount.RuleID, Kind: discount.Kind, Amount: discount.Amount, PromoCode: discount.PromoCode,
			})
		}
		out.Lines = append(out.Lines, priced)
	}
	return out, nil
}

func (p *pricer) Redeem(ctx context.Context, code, orderID, customerID string, subtotal int64, currency string) error {
	_, err := p.redeem.Handle(ctx, command.RedeemPromoCode{
		Code: code, OrderID: orderID, CustomerID: customerID, Subtotal: subtotal, Currency: currency,
	})
	return err
}

func (p *pricer) Release(ctx context.Context, code, orderID string) error {
	_, err := p.release.Handle(ctx, command.ReleasePromoCode{Code: code, OrderID: orderID})
	return err
}

func decorate[C any, R any](d Dependencies, h cqrs.Handler[C, R]) cqrs.Handler[C, R] {
	return cqrs.Decorate(moduleName, h, d.Logger, d.Metrics)
}
