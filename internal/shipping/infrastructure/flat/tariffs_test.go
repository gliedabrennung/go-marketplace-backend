package flat_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shipping/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shipping/infrastructure/flat"
)

func TestFlatTariffs(t *testing.T) {
	tariffs := flat.New(flat.Settings{Fee: 99000, FreeFrom: 1000000})
	quote, err := tariffs.Quote(context.Background(), api.TariffRequest{
		Currency: "KZT",
		Parcels:  []api.Parcel{{SellerID: "a", Subtotal: 500000}, {SellerID: "b", Subtotal: 1000000}},
	})
	require.NoError(t, err)
	assert.Equal(t, api.TariffQuote{
		Method: api.MethodStandard, Currency: "KZT", Total: 99000,
		Parcels: []api.ParcelCost{{SellerID: "a", Cost: 99000}, {SellerID: "b", Cost: 0}},
	}, quote)

	_, err = tariffs.Quote(context.Background(), api.TariffRequest{Method: "drone"})
	require.ErrorIs(t, err, api.ErrUnknownMethod)

	always := flat.New(flat.Settings{Fee: 500})
	quote, err = always.Quote(context.Background(), api.TariffRequest{Method: api.MethodStandard, Parcels: []api.Parcel{{SellerID: "a", Subtotal: 1 << 40}}})
	require.NoError(t, err)
	assert.Equal(t, int64(500), quote.Total)
}
