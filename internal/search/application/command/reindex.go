package command

import (
	"context"
	"time"

	catalogapi "github.com/gliedabrennung/go-marketplace-backend/internal/catalog/api"
	identity "github.com/gliedabrennung/go-marketplace-backend/internal/identity/api"
	"github.com/gliedabrennung/go-marketplace-backend/internal/search/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type RequestReindex struct {
	Actor auth.Principal
}

type RequestReindexResult struct {
	JobID string
}

type RequestReindexHandler struct {
	base Base
	jobs application.ReindexJobs
}

func NewRequestReindexHandler(base Base, jobs application.ReindexJobs) *RequestReindexHandler {
	return &RequestReindexHandler{base: base, jobs: jobs}
}

func (h *RequestReindexHandler) Handle(ctx context.Context, cmd RequestReindex) (RequestReindexResult, error) {
	if err := identity.Authorize(cmd.Actor, identity.PermCategoriesManage); err != nil {
		return RequestReindexResult{}, err
	}
	actor, err := kernel.ParseUserID(cmd.Actor.UserID)
	if err != nil {
		return RequestReindexResult{}, auth.ErrInvalidToken
	}
	unfinished, err := h.jobs.HasUnfinished(ctx)
	if err != nil {
		return RequestReindexResult{}, err
	}
	if unfinished {
		return RequestReindexResult{}, application.ErrReindexRunning
	}
	target, err := h.base.index.InactiveTable(ctx)
	if err != nil {
		return RequestReindexResult{}, err
	}
	job := application.ReindexJob{
		ID: kernel.NewID[struct{}]().String(), Status: application.ReindexPending, Target: target,
		RequestedBy: actor.String(), CreatedAt: h.base.clock.Now(),
	}
	if err := h.jobs.Create(ctx, job); err != nil {
		return RequestReindexResult{}, err
	}
	return RequestReindexResult{JobID: job.ID}, nil
}

type RunReindex struct{}

type RunReindexResult struct {
	JobID     string
	Status    string
	Processed int
}

type RunReindexHandler struct {
	base Base
	jobs application.ReindexJobs
	feed catalogapi.Feed
}

func NewRunReindexHandler(base Base, jobs application.ReindexJobs, feed catalogapi.Feed) *RunReindexHandler {
	return &RunReindexHandler{base: base, jobs: jobs, feed: feed}
}

func (h *RunReindexHandler) Handle(ctx context.Context, _ RunReindex) (RunReindexResult, error) {
	started := h.base.clock.Now()
	job, claimed, err := h.jobs.Claim(ctx, started)
	if err != nil || !claimed {
		return RunReindexResult{}, err
	}
	processed, err := h.rebuild(ctx, job, started)
	if err != nil {
		if finishErr := h.jobs.Finish(ctx, job.ID, application.ReindexFailed, err.Error(), h.base.clock.Now()); finishErr != nil {
			return RunReindexResult{}, finishErr
		}
		return RunReindexResult{JobID: job.ID, Status: application.ReindexFailed, Processed: processed}, nil
	}
	if err := h.jobs.Finish(ctx, job.ID, application.ReindexCompleted, "", h.base.clock.Now()); err != nil {
		return RunReindexResult{}, err
	}
	return RunReindexResult{JobID: job.ID, Status: application.ReindexCompleted, Processed: processed}, nil
}

func (h *RunReindexHandler) rebuild(ctx context.Context, job application.ReindexJob, started time.Time) (int, error) {
	if err := h.base.index.Truncate(ctx, job.Target); err != nil {
		return 0, err
	}
	processed, err := h.copy(ctx, job.ID, job.Target, catalogapi.FeedCursor{}, 0)
	if err != nil {
		return processed, err
	}
	if err := h.base.index.Swap(ctx, job.Target, h.base.clock.Now()); err != nil {
		return processed, err
	}
	processed, err = h.copy(ctx, job.ID, job.Target, catalogapi.FeedCursor{Since: started}, processed)
	if err != nil {
		return processed, err
	}
	if _, err := h.base.index.RefreshLexicon(ctx, h.base.policy.LexiconMinWeight); err != nil {
		return processed, err
	}
	return processed, nil
}

func (h *RunReindexHandler) copy(ctx context.Context, jobID, table string, cursor catalogapi.FeedCursor, processed int) (int, error) {
	for {
		batch, err := h.feed.ScanPublished(ctx, cursor, h.base.policy.FeedBatch)
		if err != nil {
			return processed, err
		}
		if len(batch) == 0 {
			return processed, nil
		}
		if err := h.base.index.SaveDocuments(ctx, table, application.NewDocuments(batch), h.base.clock.Now()); err != nil {
			return processed, err
		}
		processed += len(batch)
		cursor.AfterID = batch[len(batch)-1].ProductID
		if err := h.jobs.Progress(ctx, jobID, processed, h.base.clock.Now()); err != nil {
			return processed, err
		}
	}
}

type GetReindexJob struct {
	Actor auth.Principal
	JobID string
}

type GetReindexJobHandler struct {
	jobs application.ReindexJobs
}

func NewGetReindexJobHandler(jobs application.ReindexJobs) *GetReindexJobHandler {
	return &GetReindexJobHandler{jobs: jobs}
}

func (h *GetReindexJobHandler) Handle(ctx context.Context, q GetReindexJob) (application.ReindexJob, error) {
	if err := identity.Authorize(q.Actor, identity.PermCategoriesManage); err != nil {
		return application.ReindexJob{}, err
	}
	if _, err := kernel.ParseID[struct{}](q.JobID); err != nil {
		return application.ReindexJob{}, application.ErrReindexJobNotFound
	}
	return h.jobs.Get(ctx, q.JobID)
}
