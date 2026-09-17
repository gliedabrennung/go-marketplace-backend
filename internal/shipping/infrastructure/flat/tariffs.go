package flat

import (
	"context"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shipping/api"
)

type Settings struct {
	Fee      int64
	FreeFrom int64
}

type Tariffs struct {
	settings Settings
}

func New(settings Settings) *Tariffs {
	return &Tariffs{settings: settings}
}

func (t *Tariffs) Quote(_ context.Context, request api.TariffRequest) (api.TariffQuote, error) {
	method := request.Method
	if method == "" {
		method = api.MethodStandard
	}
	if method != api.MethodStandard {
		return api.TariffQuote{}, api.ErrUnknownMethod.WithDetail("method %q", request.Method)
	}
	quote := api.TariffQuote{Method: method, Currency: request.Currency, Parcels: make([]api.ParcelCost, 0, len(request.Parcels))}
	for _, parcel := range request.Parcels {
		cost := t.settings.Fee
		if t.settings.FreeFrom > 0 && parcel.Subtotal >= t.settings.FreeFrom {
			cost = 0
		}
		quote.Parcels = append(quote.Parcels, api.ParcelCost{SellerID: parcel.SellerID, Cost: cost})
		quote.Total += cost
	}
	return quote, nil
}
