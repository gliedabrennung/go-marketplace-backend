package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/application"
	"github.com/gliedabrennung/go-marketplace-backend/internal/identity/domain"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/platform/auth"
)

type SessionStarter struct {
	uow     application.UnitOfWork
	secrets application.SecretGenerator
	tokens  application.AccessTokenIssuer
	policy  application.Policy
}

func NewSessionStarter(uow application.UnitOfWork, secrets application.SecretGenerator, tokens application.AccessTokenIssuer, policy application.Policy) *SessionStarter {
	return &SessionStarter{uow: uow, secrets: secrets, tokens: tokens, policy: policy}
}

func (s *SessionStarter) Start(ctx context.Context, user *domain.User, device domain.Device, now time.Time) (application.AuthTokens, error) {
	refresh, err := s.secrets.Token()
	if err != nil {
		return application.AuthTokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	session, err := domain.StartSession(domain.NewSessionID(), user.ID(), device, refresh, s.policy.RefreshTTL, now)
	if err != nil {
		return application.AuthTokens{}, err
	}
	err = s.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		return repos.Sessions().Save(ctx, session)
	})
	if err != nil {
		return application.AuthTokens{}, err
	}
	return s.Issue(user, session, refresh, now)
}

func (s *SessionStarter) Issue(user *domain.User, session *domain.Session, refresh string, now time.Time) (application.AuthTokens, error) {
	access, expiresAt, err := s.tokens.Issue(auth.Principal{
		UserID:    user.ID().String(),
		SessionID: session.ID().String(),
		Roles:     user.RoleNames(),
	}, now)
	if err != nil {
		return application.AuthTokens{}, fmt.Errorf("issue access token: %w", err)
	}
	return application.AuthTokens{
		UserID:           user.ID().String(),
		SessionID:        session.ID().String(),
		AccessToken:      access,
		AccessExpiresAt:  expiresAt,
		RefreshToken:     refresh,
		RefreshExpiresAt: session.ExpiresAt(),
	}, nil
}

func (s *SessionStarter) RefreshTTL() time.Duration {
	return s.policy.RefreshTTL
}

type EmailConfirmations struct {
	uow     application.UnitOfWork
	secrets application.SecretGenerator
	sender  application.ConfirmationSender
	policy  application.Policy
}

func NewEmailConfirmations(uow application.UnitOfWork, secrets application.SecretGenerator, sender application.ConfirmationSender, policy application.Policy) *EmailConfirmations {
	return &EmailConfirmations{uow: uow, secrets: secrets, sender: sender, policy: policy}
}

func (c *EmailConfirmations) Issue(ctx context.Context, user *domain.User, now time.Time) error {
	secret, err := c.secrets.Token()
	if err != nil {
		return fmt.Errorf("generate confirmation token: %w", err)
	}
	id := domain.NewChallengeID()
	challenge, err := domain.IssueEmailConfirmationChallenge(id, user.ID(), user.Email(), secret, c.policy.EmailConfirmation, now)
	if err != nil {
		return err
	}
	err = c.uow.Do(ctx, func(ctx context.Context, repos application.Repositories) error {
		return repos.Challenges().Save(ctx, challenge)
	})
	if err != nil {
		return err
	}
	if err := c.sender.SendEmailConfirmation(ctx, user.Email(), EncodeConfirmationToken(id, secret)); err != nil {
		return fmt.Errorf("send email confirmation: %w", err)
	}
	return nil
}

func EncodeConfirmationToken(id domain.ChallengeID, secret string) string {
	return id.String() + "." + secret
}

func decodeConfirmationToken(token string) (domain.ChallengeID, string, error) {
	rawID, secret, found := strings.Cut(token, ".")
	if !found || secret == "" {
		return domain.ChallengeID{}, "", domain.ErrInvalidConfirmationToken
	}
	id, err := domain.ParseChallengeID(rawID)
	if err != nil {
		return domain.ChallengeID{}, "", domain.ErrInvalidConfirmationToken
	}
	return id, secret, nil
}

func actorID(actor auth.Principal) (kernel.UserID, error) {
	if actor.UserID == "" {
		return kernel.UserID{}, auth.ErrUnauthenticated
	}
	id, err := kernel.ParseUserID(actor.UserID)
	if err != nil {
		return kernel.UserID{}, auth.ErrInvalidToken
	}
	return id, nil
}

func audit(actor auth.Principal, action, objectID string, details map[string]string, at time.Time) application.AuditEntry {
	return application.AuditEntry{
		ActorID:    actor.UserID,
		ActorRoles: actor.Roles,
		Action:     action,
		ObjectType: "identity.user",
		ObjectID:   objectID,
		Details:    details,
		OccurredAt: at,
	}
}
