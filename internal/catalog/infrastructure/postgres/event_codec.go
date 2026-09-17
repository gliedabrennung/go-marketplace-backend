package postgres

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/outbox"
)

func NewEventCodec() *outbox.Codec {
	c := outbox.NewCodec()
	registerCategoryEvents(c)
	registerProductEvents(c)
	registerOfferEvents(c)
	return c
}

func registerCategoryEvents(c *outbox.Codec) {
	outbox.Register(c, func(e domain.CategoryCreated) any {
		return api.CategoryCreatedV1{
			CategoryID: e.CategoryID.String(), ParentID: optionalCategory(e.ParentID), Name: e.Name, Slug: e.Slug,
			Path: categoryPath(e.Path), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.CategoryRenamed) any {
		return api.CategoryRenamedV1{CategoryID: e.CategoryID.String(), Name: e.Name, Slug: e.Slug, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.CategoryAttributeDefined) any {
		spec := e.Attribute.Spec()
		return api.CategoryAttributeV1{
			CategoryID: e.CategoryID.String(), Code: spec.Code, Name: spec.Name, Type: spec.Type, Required: spec.Required,
			Filterable: spec.Filterable, Options: spec.Options, Unit: spec.Unit, OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.CategoryAttributeRemoved) any {
		return api.CategoryAttributeV1{CategoryID: e.CategoryID.String(), Code: e.Code, OccurredAt: e.At.UTC()}
	})
}

func registerProductEvents(c *outbox.Codec) {
	outbox.Register(c, func(e domain.ProductCreated) any {
		return api.ProductCreatedV1{
			ProductID: e.ProductID.String(), CategoryID: e.CategoryID.String(), SellerID: e.SellerID.String(),
			Title: e.Title, OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.ProductContentUpdated) any {
		return api.ProductContentUpdatedV1{ProductID: e.ProductID.String(), Title: e.Title, OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.ProductSubmitted) any {
		return api.ProductSubmittedV1{ProductID: e.ProductID.String(), SellerID: e.SellerID.String(), OccurredAt: e.At.UTC()}
	})
	outbox.Register(c, func(e domain.ProductPublished) any {
		return api.ProductPublishedV1{
			ProductID: e.ProductID.String(), SellerID: e.SellerID.String(), CategoryPath: categoryPath(e.CategoryPath),
			Title: e.Title, Description: e.Description, Brand: e.Brand, Attributes: productAttributes(e.Attributes),
			CoverKey: e.CoverKey, PublishedBy: e.PublishedBy.String(), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.ProductRejected) any {
		return api.ProductRejectedV1{
			ProductID: e.ProductID.String(), SellerID: e.SellerID.String(), Reason: e.Reason,
			RejectedBy: e.RejectedBy.String(), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.ProductImageUploaded) any {
		return api.ProductImageUploadedV1{
			ProductID: e.ProductID.String(), ImageID: e.ImageID.String(), ObjectKey: e.ObjectKey,
			ContentType: e.ContentType, OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.ProductImageProcessed) any {
		return api.ProductImageProcessedV1{
			ProductID: e.ProductID.String(), ImageID: e.ImageID.String(), CoverKey: e.CoverKey,
			Published: e.Published, OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.VariantGroupCreated) any {
		return api.VariantGroupCreatedV1{
			GroupID: e.GroupID.String(), CategoryID: e.CategoryID.String(), SellerID: e.SellerID.String(),
			Axes: e.Axes, OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.VariantGroupChanged) any {
		ids := make([]string, len(e.ProductIDs))
		for i, id := range e.ProductIDs {
			ids[i] = id.String()
		}
		return api.VariantGroupChangedV1{GroupID: e.GroupID.String(), ProductIDs: ids, OccurredAt: e.At.UTC()}
	})
}

func registerOfferEvents(c *outbox.Codec) {
	outbox.Register(c, func(e domain.OfferCreated) any { return offerPayload(e.Offer, e.At) })
	outbox.Register(c, func(e domain.OfferUpdated) any { return offerPayload(e.Offer, e.At) })
	outbox.Register(c, func(e domain.OfferStatusChanged) any { return offerPayload(e.Offer, e.At) })
	outbox.Register(c, func(e domain.ImportScheduled) any {
		return api.ImportScheduledV1{
			JobID: e.JobID.String(), SellerID: e.SellerID.String(), Format: string(e.Format), OccurredAt: e.At.UTC(),
		}
	})
	outbox.Register(c, func(e domain.ImportFinished) any {
		return api.ImportFinishedV1{
			JobID: e.JobID.String(), SellerID: e.SellerID.String(), Status: string(e.Status), Reason: e.Reason,
			TotalRows: e.TotalRows, SucceededRows: e.SucceededRows, FailedRows: e.FailedRows, OccurredAt: e.At.UTC(),
		}
	})
}

func offerPayload(state domain.OfferState, at time.Time) api.OfferV1 {
	return api.OfferV1{
		OfferID: state.OfferID.String(), ProductID: state.ProductID.String(), SellerID: state.SellerID.String(),
		SellerSKU: state.SellerSKU, PriceAmount: state.Price.Amount(), Currency: string(state.Price.Currency()),
		Condition: string(state.Condition), ProcessingDays: state.ProcessingDays, Status: string(state.Status),
		OccurredAt: at.UTC(),
	}
}

func categoryPath(path []domain.CategoryID) []string {
	out := make([]string, len(path))
	for i, id := range path {
		out[i] = id.String()
	}
	return out
}

func productAttributes(entries []domain.AttributeEntry) []api.ProductAttributeV1 {
	out := make([]api.ProductAttributeV1, 0, len(entries))
	for _, entry := range entries {
		attribute := api.ProductAttributeV1{
			Code: entry.Definition.Code(), Name: entry.Definition.Name(), Type: string(entry.Definition.Type()),
			Value: entry.Value.String(), Unit: entry.Definition.Unit(), Filterable: entry.Definition.Filterable(),
		}
		if number, ok := entry.Value.Number(); ok {
			attribute.Number = &number
		}
		out = append(out, attribute)
	}
	return out
}

func optionalCategory(id domain.CategoryID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}
