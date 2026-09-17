package command_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/command"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application/query"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/infrastructure/memory"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/clock"
)

var ctx = context.Background()

const (
	testEmail    = "buyer@example.kz"
	otherEmail   = "other@example.kz"
	testPassword = "correct horse 42"
)

type fakeSecrets struct {
	mu     sync.Mutex
	tokens int
}

func (s *fakeSecrets) Token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens++
	return fmt.Sprintf("token-%d", s.tokens), nil
}

type fakeSender struct {
	mu    sync.Mutex
	links map[string]string
	err   error
}

func (s *fakeSender) SendEmailConfirmation(_ context.Context, email domain.Email, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.links[email.String()] = token
	return nil
}

var errLimited = kernel.RateLimited("RATE_LIMIT_EXCEEDED", "too many requests")

type fakeLimiter struct {
	deny map[application.LimitedAction]bool
}

func (l *fakeLimiter) Allow(_ context.Context, action application.LimitedAction, _ string) error {
	if l.deny[action] {
		return errLimited
	}
	return nil
}

type fakeHasher struct{}

func (fakeHasher) Hash(p domain.Password) (domain.PasswordHash, error) {
	return domain.NewPasswordHash("hashed:" + p.Reveal())
}

func (fakeHasher) Verify(h domain.PasswordHash, password string) (bool, error) {
	return !h.IsZero() && h.String() == "hashed:"+password, nil
}

type fakeTokens struct{}

func (fakeTokens) Issue(p auth.Principal, now time.Time) (string, time.Time, error) {
	return "access:" + p.UserID + ":" + p.SessionID + ":" + strings.Join(p.Roles, ","), now.Add(15 * time.Minute), nil
}

type env struct {
	store      *memory.Store
	clock      *clock.Manual
	secrets    *fakeSecrets
	sender     *fakeSender
	limiter    *fakeLimiter
	registered map[string]string

	registerEmail *command.RegisterWithEmailHandler
	confirmEmail  *command.ConfirmEmailHandler
	signInEmail   *command.SignInWithEmailHandler
	refresh       *command.RefreshSessionHandler
	revokeSession *command.RevokeSessionHandler
	revokeAll     *command.RevokeUserSessionsHandler
	block         *command.BlockUserHandler
	unblock       *command.UnblockUserHandler
	grantRole     *command.GrantRoleHandler
	revokeRole    *command.RevokeRoleHandler
	listSessions  *query.ListSessionsHandler
	profile       *query.GetProfileHandler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		store:      memory.NewStore(),
		clock:      clock.NewManual(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)),
		secrets:    &fakeSecrets{},
		sender:     &fakeSender{links: map[string]string{}},
		limiter:    &fakeLimiter{deny: map[application.LimitedAction]bool{}},
		registered: map[string]string{},
	}
	uow := memory.NewUnitOfWork(e.store)
	policy := application.DefaultPolicy()
	starter := command.NewSessionStarter(uow, e.secrets, fakeTokens{}, policy)
	confirmations := command.NewEmailConfirmations(uow, e.secrets, e.sender, policy)

	e.registerEmail = command.NewRegisterWithEmailHandler(uow, fakeHasher{}, confirmations, e.limiter, e.clock)
	e.confirmEmail = command.NewConfirmEmailHandler(uow, e.clock)
	e.signInEmail = command.NewSignInWithEmailHandler(uow, fakeHasher{}, e.limiter, e.clock, starter)
	e.refresh = command.NewRefreshSessionHandler(uow, e.secrets, e.clock, starter)
	e.revokeSession = command.NewRevokeSessionHandler(uow, e.clock)
	e.revokeAll = command.NewRevokeUserSessionsHandler(uow, e.store, e.clock)
	e.block = command.NewBlockUserHandler(uow, e.clock)
	e.unblock = command.NewUnblockUserHandler(uow, e.clock)
	e.grantRole = command.NewGrantRoleHandler(uow, e.clock)
	e.revokeRole = command.NewRevokeRoleHandler(uow, e.clock)
	e.listSessions = query.NewListSessionsHandler(e.store, e.clock)
	e.profile = query.NewGetProfileHandler(e.store)
	return e
}

func (e *env) register(t *testing.T, email string) string {
	t.Helper()
	res, err := e.registerEmail.Handle(ctx, command.RegisterWithEmail{Email: email, Password: testPassword})
	require.NoError(t, err)
	_, err = e.confirmEmail.Handle(ctx, command.ConfirmEmail{Token: e.sender.links[email]})
	require.NoError(t, err)
	e.registered[email] = res.UserID
	return res.UserID
}

func (e *env) signIn(t *testing.T, email string) application.AuthTokens {
	t.Helper()
	if _, ok := e.registered[email]; !ok {
		e.register(t, email)
	}
	tokens, err := e.signInEmail.Handle(ctx, command.SignInWithEmail{
		Email: email, Password: testPassword, DeviceName: "iPhone", UserAgent: "ios", IP: "10.0.0.1",
	})
	require.NoError(t, err)
	return tokens
}

func (e *env) principal(tokens application.AuthTokens, roles ...string) auth.Principal {
	return auth.Principal{UserID: tokens.UserID, SessionID: tokens.SessionID, Roles: append([]string{"buyer"}, roles...)}
}

func (e *env) admin(t *testing.T) auth.Principal {
	t.Helper()
	return e.principal(e.signIn(t, "admin@example.kz"), "platform_admin")
}

func profileQuery(userID string) query.GetProfile {
	return query.GetProfile{Actor: auth.Principal{UserID: userID}}
}

func eventNames(events []kernel.DomainEvent) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.EventName()
	}
	return out
}
