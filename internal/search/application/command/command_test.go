package command_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
)

var ctx = context.Background()

type index struct {
	active     string
	documents  map[string]map[string]application.Document
	offers     map[string]application.OfferState
	stock      map[string]int
	sellers    map[string]bool
	refreshed  []string
	truncated  []string
	lexicon    int
	swaps      []string
	failOnSave bool
}

func newIndex() *index {
	return &index{
		active:    "documents_a",
		documents: map[string]map[string]application.Document{"documents_a": {}, "documents_b": {}},
		offers:    map[string]application.OfferState{},
		stock:     map[string]int{},
		sellers:   map[string]bool{},
	}
}

func (i *index) ActiveTable(context.Context) (string, error) { return i.active, nil }

func (i *index) InactiveTable(context.Context) (string, error) {
	if i.active == "documents_a" {
		return "documents_b", nil
	}
	return "documents_a", nil
}

func (i *index) SaveDocuments(_ context.Context, table string, documents []application.Document, _ time.Time) error {
	if i.failOnSave {
		return errors.New("index is unavailable")
	}
	for _, doc := range documents {
		i.documents[table][doc.ProductID] = doc
	}
	return nil
}

func (i *index) SetCover(_ context.Context, table, productID, coverKey string, _ time.Time) error {
	doc := i.documents[table][productID]
	doc.CoverKey = coverKey
	i.documents[table][productID] = doc
	return nil
}

func (i *index) Truncate(_ context.Context, table string) error {
	i.truncated = append(i.truncated, table)
	i.documents[table] = map[string]application.Document{}
	return nil
}

func (i *index) Swap(_ context.Context, table string, _ time.Time) error {
	i.swaps = append(i.swaps, table)
	i.active = table
	return nil
}

func (i *index) SaveOffer(_ context.Context, offer application.OfferState) error {
	i.offers[offer.OfferID] = offer
	return nil
}

func (i *index) SaveStock(_ context.Context, sku string, available int, _ time.Time) (string, error) {
	i.stock[sku] = available
	offer, ok := i.offers[sku]
	if !ok {
		return "", nil
	}
	return offer.ProductID, nil
}

func (i *index) SaveSeller(_ context.Context, state application.SellerState, _ time.Time) error {
	i.sellers[state.SellerID] = state.CanSell
	return nil
}

func (i *index) RefreshProduct(_ context.Context, productID string, _ time.Time) error {
	i.refreshed = append(i.refreshed, productID)
	return nil
}

func (i *index) RefreshSeller(_ context.Context, sellerID string, _ time.Time) error {
	i.refreshed = append(i.refreshed, sellerID)
	return nil
}

func (i *index) RefreshLexicon(context.Context, int) (int, error) {
	i.lexicon++
	return i.lexicon, nil
}

type jobs struct {
	stored map[string]application.ReindexJob
	order  []string
}

func newJobs() *jobs {
	return &jobs{stored: map[string]application.ReindexJob{}}
}

func (j *jobs) Create(_ context.Context, job application.ReindexJob) error {
	j.stored[job.ID] = job
	j.order = append(j.order, job.ID)
	return nil
}

func (j *jobs) Claim(_ context.Context, at time.Time) (application.ReindexJob, bool, error) {
	for _, id := range j.order {
		job := j.stored[id]
		if job.Status != application.ReindexPending {
			continue
		}
		job.Status, job.StartedAt = application.ReindexRunning, at
		j.stored[id] = job
		return job, true, nil
	}
	return application.ReindexJob{}, false, nil
}

func (j *jobs) Progress(_ context.Context, jobID string, processed int, _ time.Time) error {
	job := j.stored[jobID]
	job.Processed = processed
	j.stored[jobID] = job
	return nil
}

func (j *jobs) Finish(_ context.Context, jobID, status, reason string, at time.Time) error {
	job := j.stored[jobID]
	job.Status, job.FailureReason, job.FinishedAt = status, reason, at
	j.stored[jobID] = job
	return nil
}

func (j *jobs) Get(_ context.Context, jobID string) (application.ReindexJob, error) {
	job, ok := j.stored[jobID]
	if !ok {
		return application.ReindexJob{}, application.ErrReindexJobNotFound
	}
	return job, nil
}

func (j *jobs) HasUnfinished(context.Context) (bool, error) {
	for _, job := range j.stored {
		if job.Status == application.ReindexPending || job.Status == application.ReindexRunning {
			return true, nil
		}
	}
	return false, nil
}

type feed struct {
	documents []catalogapi.ProductDocument
	since     []catalogapi.ProductDocument
	err       error
}

func (f *feed) ScanPublished(_ context.Context, cursor catalogapi.FeedCursor, limit int) ([]catalogapi.ProductDocument, error) {
	if f.err != nil {
		return nil, f.err
	}
	source := f.documents
	if !cursor.Since.IsZero() {
		source = f.since
	}
	start := 0
	if cursor.AfterID != "" {
		start = slices.IndexFunc(source, func(d catalogapi.ProductDocument) bool { return d.ProductID == cursor.AfterID }) + 1
	}
	if start >= len(source) {
		return nil, nil
	}
	return source[start:min(start+limit, len(source))], nil
}

type env struct {
	index *index
	jobs  *jobs
	feed  *feed
	clock *clock.Manual

	indexProduct *command.IndexProductHandler
	setCover     *command.SetProductCoverHandler
	indexOffer   *command.IndexOfferHandler
	indexSeller  *command.IndexSellerHandler
	indexStock   *command.IndexStockHandler
	refresh      *command.RefreshLexiconHandler
	request      *command.RequestReindexHandler
	run          *command.RunReindexHandler
	get          *command.GetReindexJobHandler
}

func newEnv() *env {
	e := &env{
		index: newIndex(), jobs: newJobs(), feed: &feed{},
		clock: clock.NewManual(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)),
	}
	policy := application.DefaultPolicy()
	policy.FeedBatch = 2
	base := command.NewBase(e.index, e.clock, policy)

	e.indexProduct = command.NewIndexProductHandler(base)
	e.setCover = command.NewSetProductCoverHandler(base)
	e.indexOffer = command.NewIndexOfferHandler(base)
	e.indexSeller = command.NewIndexSellerHandler(base)
	e.indexStock = command.NewIndexStockHandler(base)
	e.refresh = command.NewRefreshLexiconHandler(base)
	e.request = command.NewRequestReindexHandler(base, e.jobs)
	e.run = command.NewRunReindexHandler(base, e.jobs, e.feed)
	e.get = command.NewGetReindexJobHandler(e.jobs)
	return e
}

func document(title string) catalogapi.ProductDocument {
	number := 256.0
	return catalogapi.ProductDocument{
		ProductID: kernel.NewID[struct{}]().String(), SellerID: kernel.NewSellerID().String(),
		CategoryID: kernel.NewID[struct{}]().String(), CategoryPath: []string{kernel.NewID[struct{}]().String()},
		Title: title, Brand: "Nova", PublishedAt: time.Now().UTC(),
		Attributes: []catalogapi.ProductAttributeV1{
			{Code: "color", Type: "enum", Value: "black"},
			{Code: "memory_gb", Type: "unit", Value: "256", Number: &number},
		},
	}
}

func admin() auth.Principal {
	return auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer", "platform_admin"}}
}

func TestIndexCommands(t *testing.T) {
	e := newEnv()
	source := document("Смартфон Nova X")

	_, err := e.indexProduct.Handle(ctx, command.IndexProduct{Document: source})
	require.NoError(t, err)
	stored := e.index.documents["documents_a"][source.ProductID]
	assert.Equal(t, "Смартфон Nova X", stored.Title)
	assert.Equal(t, map[string][]string{"color": {"black"}, "memory_gb": {"256"}}, stored.Attributes)
	assert.InDelta(t, 256.0, stored.Numbers["memory_gb"], 0.001)
	assert.Equal(t, []string{source.ProductID}, e.index.refreshed)

	_, err = e.setCover.Handle(ctx, command.SetProductCover{ProductID: source.ProductID, CoverKey: "cover.jpg"})
	require.NoError(t, err)
	assert.Equal(t, "cover.jpg", e.index.documents["documents_a"][source.ProductID].CoverKey)

	offer := application.OfferState{
		OfferID: kernel.NewID[struct{}]().String(), ProductID: source.ProductID, SellerID: source.SellerID,
		Price: 199000, Currency: "KZT", Condition: "new", Status: "active", UpdatedAt: e.clock.Now(),
	}
	_, err = e.indexOffer.Handle(ctx, command.IndexOffer{Offer: offer})
	require.NoError(t, err)
	assert.Equal(t, offer, e.index.offers[offer.OfferID])

	_, err = e.indexSeller.Handle(ctx, command.IndexSeller{SellerID: source.SellerID, CanSell: true})
	require.NoError(t, err)
	assert.True(t, e.index.sellers[source.SellerID])

	_, err = e.indexStock.Handle(ctx, command.IndexStock{SKU: offer.OfferID, Available: 7})
	require.NoError(t, err)
	assert.Equal(t, 7, e.index.stock[offer.OfferID])
	_, err = e.indexStock.Handle(ctx, command.IndexStock{SKU: "unknown-offer", Available: 3})
	require.NoError(t, err)

	count, err := e.refresh.Handle(ctx, command.RefreshLexicon{})
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	e.index.failOnSave = true
	_, err = e.indexProduct.Handle(ctx, command.IndexProduct{Document: source})
	require.ErrorContains(t, err, "index is unavailable")
}

func TestReindexLifecycle(t *testing.T) {
	e := newEnv()
	e.feed.documents = []catalogapi.ProductDocument{document("Первый"), document("Второй"), document("Третий")}
	e.feed.since = []catalogapi.ProductDocument{document("Догоняющий")}

	_, err := e.request.Handle(ctx, command.RequestReindex{Actor: auth.Principal{UserID: kernel.NewUserID().String(), Roles: []string{"buyer"}}})
	require.ErrorIs(t, err, auth.ErrForbidden)
	_, err = e.request.Handle(ctx, command.RequestReindex{Actor: auth.Principal{UserID: "bad", Roles: []string{"platform_admin"}}})
	require.ErrorIs(t, err, auth.ErrInvalidToken)

	requested, err := e.request.Handle(ctx, command.RequestReindex{Actor: admin()})
	require.NoError(t, err)
	_, err = e.request.Handle(ctx, command.RequestReindex{Actor: admin()})
	require.ErrorIs(t, err, application.ErrReindexRunning)

	result, err := e.run.Handle(ctx, command.RunReindex{})
	require.NoError(t, err)
	assert.Equal(t, requested.JobID, result.JobID)
	assert.Equal(t, application.ReindexCompleted, result.Status)
	assert.Equal(t, 4, result.Processed)
	assert.Equal(t, []string{"documents_b"}, e.index.truncated)
	assert.Equal(t, []string{"documents_b"}, e.index.swaps)
	assert.Len(t, e.index.documents["documents_b"], 4)
	assert.Equal(t, 1, e.index.lexicon)

	job, err := e.get.Handle(ctx, command.GetReindexJob{Actor: admin(), JobID: requested.JobID})
	require.NoError(t, err)
	assert.Equal(t, application.ReindexCompleted, job.Status)
	assert.Equal(t, 4, job.Processed)

	_, err = e.get.Handle(ctx, command.GetReindexJob{Actor: admin(), JobID: "bad"})
	require.ErrorIs(t, err, application.ErrReindexJobNotFound)
	_, err = e.get.Handle(ctx, command.GetReindexJob{Actor: admin(), JobID: kernel.NewID[struct{}]().String()})
	require.ErrorIs(t, err, application.ErrReindexJobNotFound)
	_, err = e.get.Handle(ctx, command.GetReindexJob{JobID: requested.JobID})
	require.ErrorIs(t, err, auth.ErrUnauthenticated)

	idle, err := e.run.Handle(ctx, command.RunReindex{})
	require.NoError(t, err)
	assert.Empty(t, idle.JobID)
}

func TestReindexFailure(t *testing.T) {
	e := newEnv()
	e.feed.err = errors.New("catalog feed is unavailable")

	requested, err := e.request.Handle(ctx, command.RequestReindex{Actor: admin()})
	require.NoError(t, err)

	result, err := e.run.Handle(ctx, command.RunReindex{})
	require.NoError(t, err)
	assert.Equal(t, application.ReindexFailed, result.Status)

	job, err := e.get.Handle(ctx, command.GetReindexJob{Actor: admin(), JobID: requested.JobID})
	require.NoError(t, err)
	assert.Equal(t, application.ReindexFailed, job.Status)
	assert.Contains(t, job.FailureReason, "catalog feed is unavailable")
	assert.Empty(t, e.index.swaps)
}
