package importfile

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
)

type Putter interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
}

type ReportWriter struct {
	objects Putter
}

func NewReportWriter(objects Putter) *ReportWriter {
	return &ReportWriter{objects: objects}
}

func (w *ReportWriter) Write(ctx context.Context, key string, rows []domain.ImportRowError) error {
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	out := csv.NewWriter(&buf)
	records := make([][]string, 0, len(rows)+1)
	records = append(records, []string{"row", "field", "code", "message"})
	for _, r := range rows {
		records = append(records, []string{strconv.Itoa(r.Row), safeCell(r.Field), safeCell(r.Code), safeCell(r.Message)})
	}
	if err := out.WriteAll(records); err != nil {
		return fmt.Errorf("encode import report: %w", err)
	}
	return w.objects.Put(ctx, key, "text/csv; charset=utf-8", buf.Bytes())
}

func safeCell(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}
