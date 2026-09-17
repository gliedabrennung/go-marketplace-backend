package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application/query"
)

type categoryRequest struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
}

type attributeRequest struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Required   bool     `json:"required"`
	Filterable bool     `json:"filterable"`
	Options    []string `json:"options"`
	Unit       string   `json:"unit"`
}

type productRequest struct {
	CategoryID  string            `json:"category_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Brand       string            `json:"brand"`
	Attributes  map[string]string `json:"attributes"`
}

type reasonRequest struct {
	Reason string `json:"reason"`
}

type imageRequest struct {
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type imageOrderRequest struct {
	ImageIDs []string `json:"image_ids"`
}

type variantGroupRequest struct {
	CategoryID string   `json:"category_id"`
	Axes       []string `json:"axes"`
}

type variantMemberRequest struct {
	ProductID string `json:"product_id"`
}

type offerRequest struct {
	ProductID      string `json:"product_id"`
	SellerSKU      string `json:"seller_sku"`
	Price          int64  `json:"price"`
	Currency       string `json:"currency"`
	Condition      string `json:"condition"`
	ProcessingDays int    `json:"processing_days"`
}

type offerStatusRequest struct {
	Status string `json:"status"`
}

type importUploadRequest struct {
	Format string `json:"format"`
	Size   int64  `json:"size"`
}

type importRowRequest struct {
	SellerSKU      string `json:"seller_sku"`
	ProductID      string `json:"product_id"`
	Price          string `json:"price"`
	Currency       string `json:"currency"`
	Condition      string `json:"condition"`
	ProcessingDays string `json:"processing_days"`
	Status         string `json:"status"`
}

type bulkOffersRequest struct {
	SellerID  string             `json:"seller_id"`
	Format    string             `json:"format"`
	ObjectKey string             `json:"object_key"`
	Rows      []importRowRequest `json:"rows"`
}

type categoryCreatedResponse struct {
	CategoryID string `json:"category_id"`
}

type productCreatedResponse struct {
	ProductID string `json:"product_id"`
}

type variantGroupCreatedResponse struct {
	GroupID string `json:"group_id"`
}

type offerCreatedResponse struct {
	OfferID string `json:"offer_id"`
}

type importScheduledResponse struct {
	JobID string `json:"job_id"`
}

type uploadTargetResponse struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type imageUploadResponse struct {
	ImageID string               `json:"image_id"`
	Upload  uploadTargetResponse `json:"upload"`
}

type importUploadResponse struct {
	ObjectKey string               `json:"object_key"`
	Upload    uploadTargetResponse `json:"upload"`
}

type categoryNodeResponse struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Depth    int    `json:"depth"`
}

type attributeResponse struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Required   bool     `json:"required"`
	Filterable bool     `json:"filterable"`
	Options    []string `json:"options,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	CategoryID string   `json:"category_id"`
}

type categoryResponse struct {
	ID         string                 `json:"id"`
	ParentID   string                 `json:"parent_id,omitempty"`
	Name       string                 `json:"name"`
	Slug       string                 `json:"slug"`
	Path       []categoryNodeResponse `json:"path"`
	Attributes []attributeResponse    `json:"attributes"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

type productAttributeResponse struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type imageResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	OriginalURL string `json:"original_url,omitempty"`
	SmallURL    string `json:"small_url,omitempty"`
	LargeURL    string `json:"large_url,omitempty"`
}

type variantMemberResponse struct {
	ProductID  string            `json:"product_id"`
	AxisValues map[string]string `json:"axis_values"`
}

type variantsResponse struct {
	GroupID string                  `json:"group_id"`
	Axes    []string                `json:"axes"`
	Members []variantMemberResponse `json:"members"`
}

type productResponse struct {
	ID              string                     `json:"id"`
	CategoryID      string                     `json:"category_id"`
	SellerID        string                     `json:"seller_id"`
	Title           string                     `json:"title"`
	Description     string                     `json:"description,omitempty"`
	Brand           string                     `json:"brand,omitempty"`
	Status          string                     `json:"status"`
	RejectionReason string                     `json:"rejection_reason,omitempty"`
	Attributes      []productAttributeResponse `json:"attributes"`
	Images          []imageResponse            `json:"images"`
	Variants        *variantsResponse          `json:"variants,omitempty"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
	PublishedAt     *time.Time                 `json:"published_at,omitempty"`
}

type productSummaryResponse struct {
	ID         string    `json:"id"`
	CategoryID string    `json:"category_id"`
	SellerID   string    `json:"seller_id"`
	Title      string    `json:"title"`
	Brand      string    `json:"brand,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type offerResponse struct {
	ID             string    `json:"id"`
	ProductID      string    `json:"product_id"`
	SellerID       string    `json:"seller_id"`
	SellerSKU      string    `json:"seller_sku"`
	Price          int64     `json:"price"`
	Currency       string    `json:"currency"`
	Condition      string    `json:"condition"`
	ProcessingDays int       `json:"processing_days"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type importErrorResponse struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type importJobResponse struct {
	ID            string                `json:"id"`
	SellerID      string                `json:"seller_id"`
	Format        string                `json:"format"`
	Status        string                `json:"status"`
	TotalRows     int                   `json:"total_rows"`
	SucceededRows int                   `json:"succeeded_rows"`
	FailedRows    int                   `json:"failed_rows"`
	FailureReason string                `json:"failure_reason,omitempty"`
	ReportURL     string                `json:"report_url,omitempty"`
	Errors        []importErrorResponse `json:"errors,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	StartedAt     *time.Time            `json:"started_at,omitempty"`
	FinishedAt    *time.Time            `json:"finished_at,omitempty"`
}

func toUploadTarget(target application.UploadTarget) uploadTargetResponse {
	return uploadTargetResponse{Method: target.Method, URL: target.URL, Headers: target.Headers, ExpiresAt: target.ExpiresAt}
}

func toCategoryNode(node query.CategoryNode) categoryNodeResponse {
	return categoryNodeResponse{ID: node.ID, ParentID: node.ParentID, Name: node.Name, Slug: node.Slug, Depth: node.Depth}
}

func toCategory(view query.CategoryView) categoryResponse {
	out := categoryResponse{
		ID: view.ID, ParentID: view.ParentID, Name: view.Name, Slug: view.Slug,
		CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
		Path:       make([]categoryNodeResponse, 0, len(view.Path)),
		Attributes: make([]attributeResponse, 0, len(view.Attributes)),
	}
	for _, node := range view.Path {
		out.Path = append(out.Path, toCategoryNode(node))
	}
	for _, a := range view.Attributes {
		out.Attributes = append(out.Attributes, attributeResponse{
			Code: a.Code, Name: a.Name, Type: a.Type, Required: a.Required, Filterable: a.Filterable,
			Options: a.Options, Unit: a.Unit, CategoryID: a.CategoryID,
		})
	}
	return out
}

func toProduct(view query.ProductView) productResponse {
	out := productResponse{
		ID: view.ID, CategoryID: view.CategoryID, SellerID: view.SellerID, Title: view.Title,
		Description: view.Description, Brand: view.Brand, Status: view.Status, RejectionReason: view.RejectionReason,
		CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
		Attributes: make([]productAttributeResponse, 0, len(view.Attributes)),
		Images:     make([]imageResponse, 0, len(view.Images)),
	}
	if !view.PublishedAt.IsZero() {
		published := view.PublishedAt
		out.PublishedAt = &published
	}
	for _, a := range view.Attributes {
		out.Attributes = append(out.Attributes, productAttributeResponse{Code: a.Code, Name: a.Name, Type: a.Type, Value: a.Value, Unit: a.Unit})
	}
	for _, img := range view.Images {
		out.Images = append(out.Images, imageResponse{
			ID: img.ID, Status: img.Status, ContentType: img.ContentType, Width: img.Width, Height: img.Height,
			OriginalURL: img.OriginalURL, SmallURL: img.SmallURL, LargeURL: img.LargeURL,
		})
	}
	if view.Variants != nil {
		variants := variantsResponse{GroupID: view.Variants.GroupID, Axes: view.Variants.Axes,
			Members: make([]variantMemberResponse, 0, len(view.Variants.Members))}
		for _, m := range view.Variants.Members {
			variants.Members = append(variants.Members, variantMemberResponse{ProductID: m.ProductID, AxisValues: m.AxisValues})
		}
		out.Variants = &variants
	}
	return out
}

func toProductSummary(view query.ProductSummary) productSummaryResponse {
	return productSummaryResponse{
		ID: view.ID, CategoryID: view.CategoryID, SellerID: view.SellerID, Title: view.Title, Brand: view.Brand,
		Status: view.Status, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
	}
}

func toOffer(view query.OfferView) offerResponse {
	return offerResponse{
		ID: view.ID, ProductID: view.ProductID, SellerID: view.SellerID, SellerSKU: view.SellerSKU, Price: view.Price,
		Currency: view.Currency, Condition: view.Condition, ProcessingDays: view.ProcessingDays, Status: view.Status,
		CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
	}
}

func toImportJob(view query.ImportJobView) importJobResponse {
	out := importJobResponse{
		ID: view.ID, SellerID: view.SellerID, Format: view.Format, Status: view.Status, TotalRows: view.TotalRows,
		SucceededRows: view.SucceededRows, FailedRows: view.FailedRows, FailureReason: view.FailureReason,
		ReportURL: view.ReportURL, CreatedAt: view.CreatedAt,
	}
	if !view.StartedAt.IsZero() {
		started := view.StartedAt
		out.StartedAt = &started
	}
	if !view.FinishedAt.IsZero() {
		finished := view.FinishedAt
		out.FinishedAt = &finished
	}
	for _, e := range view.Errors {
		out.Errors = append(out.Errors, importErrorResponse{Row: e.Row, Field: e.Field, Code: e.Code, Message: e.Message})
	}
	return out
}

func toImportRecords(rows []importRowRequest) []command.ImportRecord {
	if len(rows) == 0 {
		return nil
	}
	out := make([]command.ImportRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, command.ImportRecord{
			SellerSKU: r.SellerSKU, ProductID: r.ProductID, Price: r.Price, Currency: r.Currency,
			Condition: r.Condition, ProcessingDays: r.ProcessingDays, Status: r.Status,
		})
	}
	return out
}
