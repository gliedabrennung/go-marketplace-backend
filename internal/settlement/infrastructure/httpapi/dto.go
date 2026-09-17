package httpapi

import (
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/settlement/application/query"
)

type entryDTO struct {
	ID         string    `json:"id"`
	OrderID    string    `json:"order_id"`
	Currency   string    `json:"currency"`
	Gross      int64     `json:"gross"`
	Commission int64     `json:"commission"`
	Net        int64     `json:"net"`
	AccruedAt  time.Time `json:"accrued_at"`
}

type reportDTO struct {
	SellerID   string     `json:"seller_id"`
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
	Currency   string     `json:"currency"`
	Gross      int64      `json:"gross"`
	Commission int64      `json:"commission"`
	Net        int64      `json:"net"`
	Entries    []entryDTO `json:"entries"`
}

func toReport(report query.Report) reportDTO {
	out := reportDTO{
		SellerID: report.SellerID, From: report.From, To: report.To, Currency: report.Currency, Gross: report.Gross,
		Commission: report.Commission, Net: report.Net, Entries: make([]entryDTO, 0, len(report.Entries)),
	}
	for _, entry := range report.Entries {
		out.Entries = append(out.Entries, entryDTO(entry))
	}
	return out
}
