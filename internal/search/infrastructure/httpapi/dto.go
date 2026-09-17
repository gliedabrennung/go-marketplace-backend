package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/httpx"
)

type productHitResponse struct {
	ProductID   string    `json:"product_id"`
	SellerID    string    `json:"seller_id"`
	CategoryID  string    `json:"category_id"`
	Title       string    `json:"title"`
	Brand       string    `json:"brand,omitempty"`
	CoverKey    string    `json:"cover_key,omitempty"`
	MinPrice    int64     `json:"min_price"`
	Currency    string    `json:"currency"`
	Offers      int       `json:"offers"`
	Sellers     int       `json:"sellers"`
	InStock     bool      `json:"in_stock"`
	PublishedAt time.Time `json:"published_at"`
	Rank        float64   `json:"rank,omitempty"`
}

type searchResponse struct {
	Data      []productHitResponse `json:"data"`
	Page      httpx.PageInfo       `json:"page"`
	Corrected string               `json:"corrected_query,omitempty"`
}

type facetValueResponse struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type attributeFacetResponse struct {
	Code   string               `json:"code"`
	Values []facetValueResponse `json:"values"`
}

type numberFacetResponse struct {
	Code string  `json:"code"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

type facetsResponse struct {
	Total      int                      `json:"total"`
	PriceMin   int64                    `json:"price_min"`
	PriceMax   int64                    `json:"price_max"`
	Attributes []attributeFacetResponse `json:"attributes"`
	Numbers    []numberFacetResponse    `json:"numbers"`
	Conditions []facetValueResponse     `json:"conditions"`
}

type reindexScheduledResponse struct {
	JobID string `json:"job_id"`
}

type reindexJobResponse struct {
	ID            string     `json:"id"`
	Status        string     `json:"status"`
	Target        string     `json:"target"`
	Processed     int        `json:"processed"`
	FailureReason string     `json:"failure_reason,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

func toSearchResponse(result query.SearchResult) searchResponse {
	page := httpx.NewPageResponse(result.Page, toHit)
	return searchResponse{Data: page.Data, Page: page.Page, Corrected: result.Corrected}
}

func toHit(hit query.ProductHit) productHitResponse {
	return productHitResponse{
		ProductID: hit.ProductID, SellerID: hit.SellerID, CategoryID: hit.CategoryID, Title: hit.Title,
		Brand: hit.Brand, CoverKey: hit.CoverKey, MinPrice: hit.MinPrice, Currency: hit.Currency,
		Offers: hit.Offers, Sellers: hit.Sellers, InStock: hit.InStock, PublishedAt: hit.PublishedAt, Rank: hit.Rank,
	}
}

func toFacets(set query.FacetSet) facetsResponse {
	out := facetsResponse{
		Total: set.Total, PriceMin: set.PriceMin, PriceMax: set.PriceMax,
		Attributes: make([]attributeFacetResponse, 0, len(set.Attributes)),
		Numbers:    make([]numberFacetResponse, 0, len(set.Numbers)),
		Conditions: make([]facetValueResponse, 0, len(set.Conditions)),
	}
	for _, facet := range set.Attributes {
		values := make([]facetValueResponse, 0, len(facet.Values))
		for _, value := range facet.Values {
			values = append(values, facetValueResponse{Value: value.Value, Count: value.Count})
		}
		out.Attributes = append(out.Attributes, attributeFacetResponse{Code: facet.Code, Values: values})
	}
	for _, facet := range set.Numbers {
		out.Numbers = append(out.Numbers, numberFacetResponse{Code: facet.Code, Min: facet.Min, Max: facet.Max})
	}
	for _, value := range set.Conditions {
		out.Conditions = append(out.Conditions, facetValueResponse{Value: value.Value, Count: value.Count})
	}
	return out
}

func toReindexJob(job application.ReindexJob) reindexJobResponse {
	out := reindexJobResponse{
		ID: job.ID, Status: job.Status, Target: job.Target, Processed: job.Processed,
		FailureReason: job.FailureReason, CreatedAt: job.CreatedAt,
	}
	if !job.StartedAt.IsZero() {
		started := job.StartedAt
		out.StartedAt = &started
	}
	if !job.FinishedAt.IsZero() {
		finished := job.FinishedAt
		out.FinishedAt = &finished
	}
	return out
}
