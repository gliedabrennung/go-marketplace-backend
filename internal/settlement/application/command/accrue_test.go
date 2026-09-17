package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sellerapi "github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
)

var ctx = context.Background()

type rates map[string]int

func (r rates) Seller(context.Context, string) (sellerapi.SellerInfo, error) {
	return sellerapi.SellerInfo{}, nil
}

func (r rates) MemberRole(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}

func (r rates) CommissionRate(_ context.Context, sellerID, categoryID string) (int, error) {
	if rate, ok := r[sellerID+"/"+categoryID]; ok {
		return rate, nil
	}
	return 0, sellerapi.ErrSellerNotFound
}

func TestAccrueSettlement(t *testing.T) {
	store := memory.NewStore()
	sellerA, sellerB := kernel.NewSellerID().String(), kernel.NewSellerID().String()
	categoryA := kernel.NewSellerID().String()
	r := rates{sellerA + "/" + categoryA: 1000}
	base := command.NewBase(memory.NewUnitOfWork(store), clock.NewManual(time.Now()), r, application.DefaultPolicy())
	handler := command.NewAccrueSettlementHandler(base)

	accrued, err := handler.Handle(ctx, command.AccrueSettlement{
		OrderID: "order-1", Currency: "KZT",
		Items: []command.AccrueItem{
			{SellerID: sellerA, CategoryID: categoryA, Amount: 10000},
			{SellerID: sellerA, CategoryID: categoryA, Amount: 5000},
			{SellerID: sellerB, CategoryID: "unknown", Amount: 20000},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, accrued)

	entryA, err := store.Entries().FindByOrderAndSeller(ctx, "order-1", sellerA)
	require.NoError(t, err)
	assert.Equal(t, int64(15000), entryA.Gross().Amount())
	assert.Equal(t, int64(1500), entryA.Commission().Amount())
	assert.Equal(t, int64(13500), entryA.Net().Amount())

	entryB, err := store.Entries().FindByOrderAndSeller(ctx, "order-1", sellerB)
	require.NoError(t, err)
	assert.Equal(t, int64(20000), entryB.Gross().Amount())
	assert.Equal(t, int64(2000), entryB.Commission().Amount())

	again, err := handler.Handle(ctx, command.AccrueSettlement{
		OrderID: "order-1", Currency: "KZT",
		Items: []command.AccrueItem{{SellerID: sellerA, CategoryID: categoryA, Amount: 10000}},
	})
	require.NoError(t, err)
	assert.Zero(t, again)

	_, err = handler.Handle(ctx, command.AccrueSettlement{Currency: "bogus"})
	require.Error(t, err)
	_, err = handler.Handle(ctx, command.AccrueSettlement{
		Currency: "KZT", Items: []command.AccrueItem{{SellerID: "bogus", Amount: 1}},
	})
	require.ErrorIs(t, err, kernel.ErrInvalidID)
}
