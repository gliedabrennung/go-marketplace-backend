package postgres

import (
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/seller/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

func NewEventCodec() *outbox.Codec {
	c := outbox.NewCodec()
	registerLifecycleEvents(c)
	registerSettingsEvents(c)
	return c
}

func registerLifecycleEvents(c *outbox.Codec) {
	outbox.Register(c, func(e domain.SellerApplicationOpened) any {
		return api.ApplicationOpenedV1{SellerID: e.SellerID.String(), OwnerID: e.OwnerID.String(), LegalForm: string(e.LegalForm), LegalName: e.LegalName, TaxID: e.TaxID, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerLegalDetailsUpdated) any {
		return api.LegalDetailsUpdatedV1{SellerID: e.SellerID.String(), LegalName: e.LegalName, TaxID: e.TaxID, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerDocumentAttached) any {
		return api.DocumentAttachedV1{SellerID: e.SellerID.String(), Kind: string(e.Kind), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerApplicationSubmitted) any {
		return api.SellerActionV1{SellerID: e.SellerID.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerApproved) any {
		return api.ApplicationApprovedV1{SellerID: e.SellerID.String(), OwnerID: e.OwnerID.String(), ApprovedBy: e.ApprovedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerApplicationRejected) any {
		return api.SellerActionV1{SellerID: e.SellerID.String(), ActorID: e.RejectedBy.String(), Reason: e.Reason, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerSuspended) any {
		return api.SellerActionV1{SellerID: e.SellerID.String(), ActorID: optionalID(e.SuspendedBy), Reason: string(e.Reason), Note: e.Note, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerReinstated) any {
		return api.SellerActionV1{SellerID: e.SellerID.String(), ActorID: e.ReinstatedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerTerminated) any {
		return api.SellerActionV1{SellerID: e.SellerID.String(), ActorID: e.TerminatedBy.String(), Reason: e.Reason, OccurredAt: e.At.UTC()}
	})
}

func registerSettingsEvents(c *outbox.Codec) {
	outbox.Register(c, func(e domain.SellerBankAccountChanged) any {
		return api.BankAccountChangedV1{SellerID: e.SellerID.String(), MaskedIBAN: e.MaskedIBAN, ChangedBy: e.ChangedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerBankAccountVerified) any {
		return api.SellerActionV1{SellerID: e.SellerID.String(), ActorID: e.VerifiedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerMemberAdded) any {
		return api.MemberChangedV1{SellerID: e.SellerID.String(), UserID: e.UserID.String(), Role: string(e.Role), ChangedBy: e.AddedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerMemberRemoved) any {
		return api.MemberChangedV1{SellerID: e.SellerID.String(), UserID: e.UserID.String(), ChangedBy: e.RemovedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerCommissionOverrideSet) any {
		rate := e.Rate.Value()
		return api.CommissionOverrideV1{SellerID: e.SellerID.String(), CategoryID: e.CategoryID.String(), BasisPoints: &rate, ChangedBy: e.SetBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerCommissionOverrideCleared) any {
		return api.CommissionOverrideV1{SellerID: e.SellerID.String(), CategoryID: e.CategoryID.String(), ChangedBy: e.ClearedBy.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.SellerRatingChanged) any {
		return api.RatingChangedV1{SellerID: e.SellerID.String(), Score: e.Score, Provisional: e.Provisional, Orders: e.Orders, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.CategoryCommissionChanged) any {
		return api.CategoryCommissionChangedV1{CategoryID: e.CategoryID.String(), BasisPoints: e.Rate.Value(), ChangedBy: e.ChangedBy.String(), OccurredAt: e.At.UTC()}
	})
}

func optionalID(id kernel.UserID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}
