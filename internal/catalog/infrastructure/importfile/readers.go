package importfile

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/application"
)

var errNotArray = errors.New("json import must be an array of objects")

type csvReader struct {
	body   io.ReadCloser
	reader *csv.Reader
	header header
}

func newCSVReader(body io.ReadCloser) (*csvReader, error) {
	buffered := bufio.NewReader(body)
	peek, err := buffered.Peek(4096)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return nil, fmt.Errorf("read csv header: %w", err)
	}
	reader := csv.NewReader(buffered)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	firstLine, _, _ := bytes.Cut(peek, []byte("\n"))
	if bytes.Count(firstLine, []byte(";")) > bytes.Count(firstLine, []byte(",")) {
		reader.Comma = ';'
	}
	cells, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read csv header: %w", err)
	}
	h, err := parseHeader(cells)
	if err != nil {
		return nil, err
	}
	return &csvReader{body: body, reader: reader, header: h}, nil
}

func (r *csvReader) Next() (application.ImportRow, error) {
	for {
		cells, err := r.reader.Read()
		if err != nil {
			return application.ImportRow{}, err
		}
		line, _ := r.reader.FieldPos(0)
		if row, ok := r.header.row(line, cells); ok {
			return row, nil
		}
	}
}

func (r *csvReader) Close() error {
	return r.body.Close()
}

type xlsxReader struct {
	body   io.ReadCloser
	file   *excelize.File
	rows   *excelize.Rows
	header header
	line   int
}

func newXLSXReader(body io.ReadCloser) (*xlsxReader, error) {
	file, err := excelize.OpenReader(body)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	sheets := file.GetSheetList()
	if len(sheets) == 0 {
		return nil, joinClose(errors.New("xlsx has no sheets"), file)
	}
	rows, err := file.Rows(sheets[0])
	if err != nil {
		return nil, joinClose(fmt.Errorf("read xlsx rows: %w", err), file)
	}
	r := &xlsxReader{body: body, file: file, rows: rows}
	for rows.Next() {
		r.line++
		cells, err := rows.Columns()
		if err != nil {
			return nil, joinClose(fmt.Errorf("read xlsx header: %w", err), r)
		}
		if len(cells) == 0 {
			continue
		}
		if r.header, err = parseHeader(cells); err != nil {
			return nil, joinClose(err, r)
		}
		return r, nil
	}
	return nil, joinClose(errors.New("xlsx has no header row"), r)
}

func (r *xlsxReader) Next() (application.ImportRow, error) {
	for r.rows.Next() {
		r.line++
		cells, err := r.rows.Columns()
		if err != nil {
			return application.ImportRow{}, err
		}
		if row, ok := r.header.row(r.line, cells); ok {
			return row, nil
		}
	}
	if err := r.rows.Error(); err != nil {
		return application.ImportRow{}, err
	}
	return application.ImportRow{}, io.EOF
}

func (r *xlsxReader) Close() error {
	return errors.Join(r.rows.Close(), r.file.Close(), r.body.Close())
}

type field string

func (f *field) UnmarshalJSON(data []byte) error {
	switch {
	case string(data) == "null":
		*f = ""
	case len(data) > 0 && data[0] == '"':
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = field(s)
	default:
		var n json.Number
		if err := json.Unmarshal(data, &n); err != nil {
			return err
		}
		*f = field(n.String())
	}
	return nil
}

type record struct {
	SellerSKU      field `json:"seller_sku"`
	ProductID      field `json:"product_id"`
	Price          field `json:"price"`
	Currency       field `json:"currency"`
	Condition      field `json:"condition"`
	ProcessingDays field `json:"processing_days"`
	Status         field `json:"status"`
}

type jsonReader struct {
	body    io.ReadCloser
	decoder *json.Decoder
	index   int
}

func newJSONReader(body io.ReadCloser) (*jsonReader, error) {
	decoder := json.NewDecoder(bufio.NewReader(body))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read json import: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '[' {
		return nil, errNotArray
	}
	return &jsonReader{body: body, decoder: decoder}, nil
}

func (r *jsonReader) Next() (application.ImportRow, error) {
	if !r.decoder.More() {
		return application.ImportRow{}, io.EOF
	}
	var rec record
	if err := r.decoder.Decode(&rec); err != nil {
		return application.ImportRow{}, fmt.Errorf("json import item %d: %w", r.index+1, err)
	}
	r.index++
	return application.ImportRow{
		Number: r.index, SellerSKU: string(rec.SellerSKU), ProductID: string(rec.ProductID), Price: string(rec.Price),
		Currency: string(rec.Currency), Condition: string(rec.Condition), ProcessingDays: string(rec.ProcessingDays),
		Status: string(rec.Status),
	}, nil
}

func (r *jsonReader) Close() error {
	return r.body.Close()
}
