package query

import (
	"context"
	"slices"
	"time"

	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type DocumentView struct {
	Kind       string
	ObjectKey  string
	UploadedAt time.Time
}

type MemberView struct {
	UserID  string
	Role    string
	AddedAt time.Time
}

type RatingView struct {
	Score        int
	Provisional  bool
	Orders       int
	CalculatedAt time.Time
}

type SellerView struct {
	ID                  string
	OwnerID             string
	Status              string
	LegalForm           string
	LegalName           string
	TaxID               string
	LegalAddress        string
	BankIBANMasked      string
	BankBIC             string
	BankName            string
	BankVerified        bool
	Documents           []DocumentView
	Members             []MemberView
	CommissionOverrides map[string]int
	Rating              *RatingView
	RejectionReason     string
	SuspensionReason    string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type SellerSummary struct {
	ID        string
	Status    string
	LegalName string
	TaxID     string
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SellerReadModel interface {
	Seller(ctx context.Context, sellerID string) (SellerView, error)
	ListByMember(ctx context.Context, userID string) ([]SellerSummary, error)
	ListByStatus(ctx context.Context, status string, limit int, after *pagination.Keyset) (pagination.Page[SellerSummary], error)
}

type GetSeller struct {
	Actor    auth.Principal
	SellerID string
}

type GetSellerHandler struct {
	reader SellerReadModel
}

func NewGetSellerHandler(reader SellerReadModel) *GetSellerHandler {
	return &GetSellerHandler{reader: reader}
}

func (h *GetSellerHandler) Handle(ctx context.Context, q GetSeller) (SellerView, error) {
	if q.Actor.UserID == "" {
		return SellerView{}, auth.ErrUnauthenticated
	}
	view, err := h.reader.Seller(ctx, q.SellerID)
	if err != nil {
		return SellerView{}, err
	}
	member := slices.ContainsFunc(view.Members, func(m MemberView) bool { return m.UserID == q.Actor.UserID })
	staff := identity.Can(q.Actor, identity.PermSellersModerate) || identity.Can(q.Actor, identity.PermSellersManage)
	if !member && !staff {
		return SellerView{}, domain.ErrSellerNotFound
	}
	return view, nil
}

type ListMySellers struct {
	Actor auth.Principal
}

type ListMySellersHandler struct {
	reader SellerReadModel
}

func NewListMySellersHandler(reader SellerReadModel) *ListMySellersHandler {
	return &ListMySellersHandler{reader: reader}
}

func (h *ListMySellersHandler) Handle(ctx context.Context, q ListMySellers) ([]SellerSummary, error) {
	if q.Actor.UserID == "" {
		return nil, auth.ErrUnauthenticated
	}
	return h.reader.ListByMember(ctx, q.Actor.UserID)
}

type ListApplications struct {
	Actor  auth.Principal
	Status string
	Limit  int
	Cursor string
}

type ListApplicationsHandler struct {
	reader SellerReadModel
}

func NewListApplicationsHandler(reader SellerReadModel) *ListApplicationsHandler {
	return &ListApplicationsHandler{reader: reader}
}

func (h *ListApplicationsHandler) Handle(ctx context.Context, q ListApplications) (pagination.Page[SellerSummary], error) {
	if err := identity.Authorize(q.Actor, identity.PermSellersModerate); err != nil {
		return pagination.Page[SellerSummary]{}, err
	}
	status := domain.SellerStatus(q.Status)
	if q.Status == "" {
		status = domain.StatusPendingReview
	}
	if _, known := knownStatuses[status]; !known {
		return pagination.Page[SellerSummary]{}, ErrUnknownStatus.WithDetail("%q", q.Status)
	}
	keyset, ok, err := pagination.DecodeKeyset(q.Cursor)
	if err != nil {
		return pagination.Page[SellerSummary]{}, err
	}
	var after *pagination.Keyset
	if ok {
		after = &keyset
	}
	return h.reader.ListByStatus(ctx, string(status), pagination.NormalizeLimit(q.Limit), after)
}
