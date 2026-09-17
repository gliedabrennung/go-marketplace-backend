package memory

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/pagination"
)

type Store struct {
	mu         sync.Mutex
	users      map[string]domain.UserSnapshot
	sessions   map[string]domain.SessionSnapshot
	digests    map[string]string
	challenges map[string]domain.ChallengeSnapshot
	audit      []application.AuditEntry
	events     []kernel.DomainEvent
}

func NewStore() *Store {
	return &Store{
		users:      make(map[string]domain.UserSnapshot),
		sessions:   make(map[string]domain.SessionSnapshot),
		digests:    make(map[string]string),
		challenges: make(map[string]domain.ChallengeSnapshot),
	}
}

func (s *Store) Users() domain.UserRepository { return userRepository{s} }

func (s *Store) Sessions() domain.SessionRepository { return sessionRepository{s} }

func (s *Store) Challenges() domain.ChallengeRepository { return challengeRepository{s} }

func (s *Store) Audit() application.AuditTrail { return auditTrail{s} }

func (s *Store) Events() []kernel.DomainEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

func (s *Store) AuditEntries() []application.AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.audit)
}

type UnitOfWork struct {
	store *Store
}

func NewUnitOfWork(store *Store) *UnitOfWork {
	return &UnitOfWork{store: store}
}

func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context, repos application.Repositories) error) error {
	return fn(ctx, u.store)
}

type userRepository struct{ s *Store }

func (r userRepository) FindByID(_ context.Context, id kernel.UserID) (*domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.users[id.String()]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return domain.RehydrateUser(cloneUser(snap))
}

func (r userRepository) FindByVerifiedEmail(_ context.Context, email domain.Email) (*domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, snap := range r.s.users {
		if snap.EmailVerified && snap.Email == email.String() {
			return domain.RehydrateUser(cloneUser(snap))
		}
	}
	return nil, domain.ErrUserNotFound
}

func (r userRepository) Save(_ context.Context, u *domain.User) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := cloneUser(u.Snapshot())
	if err := checkVersion(r.s.users, next.ID, next.Version); err != nil {
		return err
	}
	if next.EmailVerified {
		for id, other := range r.s.users {
			if id != next.ID && other.EmailVerified && other.Email == next.Email {
				return domain.ErrEmailTaken
			}
		}
	}
	next.Version++
	r.s.users[next.ID] = next
	u.AdvanceVersion()
	r.s.events = append(r.s.events, u.PullEvents()...)
	return nil
}

func cloneUser(snap domain.UserSnapshot) domain.UserSnapshot {
	snap.Roles = slices.Clone(snap.Roles)
	return snap
}

type sessionRepository struct{ s *Store }

func (r sessionRepository) FindByID(_ context.Context, id domain.SessionID) (*domain.Session, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.sessions[id.String()]
	if !ok {
		return nil, domain.ErrSessionNotFound
	}
	return domain.RehydrateSession(snap)
}

func (r sessionRepository) FindByRefreshDigest(_ context.Context, digest string) (*domain.Session, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	id, ok := r.s.digests[digest]
	if !ok {
		return nil, domain.ErrSessionNotFound
	}
	return domain.RehydrateSession(r.s.sessions[id])
}

func (r sessionRepository) Save(_ context.Context, session *domain.Session) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := session.Snapshot()
	if err := checkVersion(r.s.sessions, next.ID, next.Version); err != nil {
		return err
	}
	next.Version++
	r.s.sessions[next.ID] = next
	r.s.digests[next.RefreshDigest] = next.ID
	session.AdvanceVersion()
	r.s.events = append(r.s.events, session.PullEvents()...)
	return nil
}

type challengeRepository struct{ s *Store }

func (r challengeRepository) FindByID(_ context.Context, id domain.ChallengeID) (*domain.Challenge, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	snap, ok := r.s.challenges[id.String()]
	if !ok {
		return nil, domain.ErrChallengeNotFound
	}
	return domain.RehydrateChallenge(snap)
}

func (r challengeRepository) Save(_ context.Context, c *domain.Challenge) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	next := c.Snapshot()
	if err := checkVersion(r.s.challenges, next.ID, next.Version); err != nil {
		return err
	}
	next.Version++
	r.s.challenges[next.ID] = next
	c.AdvanceVersion()
	return nil
}

type auditTrail struct{ s *Store }

func (a auditTrail) Record(_ context.Context, e application.AuditEntry) error {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	a.s.audit = append(a.s.audit, e)
	return nil
}

type versioned interface {
	domain.UserSnapshot | domain.SessionSnapshot | domain.ChallengeSnapshot
}

func checkVersion[T versioned](items map[string]T, id string, version int) error {
	current, exists := items[id]
	switch {
	case version == 0 && exists:
		return kernel.ErrConcurrentModification
	case version == 0:
		return nil
	case !exists || versionOf(current) != version:
		return kernel.ErrConcurrentModification
	default:
		return nil
	}
}

func versionOf[T versioned](item T) int {
	switch v := any(item).(type) {
	case domain.UserSnapshot:
		return v.Version
	case domain.SessionSnapshot:
		return v.Version
	case domain.ChallengeSnapshot:
		return v.Version
	default:
		return -1
	}
}

func (s *Store) ListActiveByUser(_ context.Context, userID string, now time.Time, limit int, after *pagination.Keyset) (pagination.Page[query.SessionView], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []query.SessionView
	for _, snap := range s.sessions {
		if snap.UserID != userID || snap.Status != string(domain.SessionStatusActive) || !now.Before(snap.ExpiresAt) {
			continue
		}
		if after != nil && !before(snap.LastUsedAt, snap.ID, after.At, after.ID) {
			continue
		}
		rows = append(rows, query.SessionView{
			ID: snap.ID, DeviceName: snap.DeviceName, UserAgent: snap.UserAgent, IP: snap.IP,
			CreatedAt: snap.CreatedAt, LastUsedAt: snap.LastUsedAt, ExpiresAt: snap.ExpiresAt,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return before(rows[j].LastUsedAt, rows[j].ID, rows[i].LastUsedAt, rows[i].ID) })
	if len(rows) > limit+1 {
		rows = rows[:limit+1]
	}
	return pagination.Build(rows, limit, func(v query.SessionView) pagination.Keyset {
		return pagination.Keyset{At: v.LastUsedAt, ID: v.ID}
	}), nil
}

func before(at time.Time, id string, pivotAt time.Time, pivotID string) bool {
	return at.Before(pivotAt) || (at.Equal(pivotAt) && id < pivotID)
}

func (s *Store) ActiveSessionIDs(_ context.Context, userID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, snap := range s.sessions {
		if snap.UserID == userID && snap.Status == string(domain.SessionStatusActive) {
			ids = append(ids, snap.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *Store) Profile(_ context.Context, userID string) (query.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.users[userID]
	if !ok {
		return query.Profile{}, domain.ErrUserNotFound
	}
	return query.Profile{
		ID: snap.ID, Email: snap.Email, EmailVerified: snap.EmailVerified,
		Roles: slices.Clone(snap.Roles), Status: snap.Status, CreatedAt: snap.CreatedAt,
	}, nil
}
