package importfile

import (
	"context"
	"io"
	"strings"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var ErrMissingColumns = kernel.Validation("CATALOG_IMPORT_MISSING_COLUMNS", "import file misses required columns")

var (
	requiredColumns = []string{"seller_sku", "product_id", "price", "condition", "processing_days"}
	optionalColumns = []string{"currency", "status"}
)

type Opener interface {
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

type Source struct {
	objects Opener
}

func NewSource(objects Opener) *Source {
	return &Source{objects: objects}
}

func (s *Source) Open(ctx context.Context, format domain.ImportFormat, key string) (application.RowReader, error) {
	body, err := s.objects.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	var reader application.RowReader
	switch format {
	case domain.ImportCSV:
		reader, err = newCSVReader(body)
	case domain.ImportXLSX:
		reader, err = newXLSXReader(body)
	case domain.ImportJSON:
		reader, err = newJSONReader(body)
	default:
		err = domain.ErrInvalidImportFormat
	}
	if err != nil {
		return nil, joinClose(err, body)
	}
	return reader, nil
}

type header map[string]int

func parseHeader(cells []string) (header, error) {
	h := header{}
	for i, cell := range cells {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(cell, "\ufeff")))
		if name != "" {
			if _, seen := h[name]; !seen {
				h[name] = i
			}
		}
	}
	var missing []string
	for _, column := range requiredColumns {
		if _, ok := h[column]; !ok {
			missing = append(missing, column)
		}
	}
	if len(missing) > 0 {
		return nil, ErrMissingColumns.WithDetail("%s", strings.Join(missing, ", "))
	}
	return h, nil
}

func (h header) row(number int, cells []string) (application.ImportRow, bool) {
	value := func(column string) string {
		idx, ok := h[column]
		if !ok || idx >= len(cells) {
			return ""
		}
		return strings.TrimSpace(cells[idx])
	}
	row := application.ImportRow{
		Number: number, SellerSKU: value("seller_sku"), ProductID: value("product_id"), Price: value("price"),
		Currency: value("currency"), Condition: value("condition"), ProcessingDays: value("processing_days"), Status: value("status"),
	}
	empty := true
	for _, column := range append(requiredColumns, optionalColumns...) {
		if value(column) != "" {
			empty = false
			break
		}
	}
	return row, !empty
}

func joinClose(err error, c io.Closer) error {
	if closeErr := c.Close(); closeErr != nil && err == nil {
		return closeErr
	}
	return err
}
