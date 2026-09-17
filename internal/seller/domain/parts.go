package domain

import (
	"strings"
	"time"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type DocumentKind string

const (
	DocumentRegistrationCertificate DocumentKind = "registration_certificate"
	DocumentCharter                 DocumentKind = "charter"
	DocumentBankConfirmation        DocumentKind = "bank_confirmation"
	DocumentIdentity                DocumentKind = "identity_document"
)

func ParseDocumentKind(s string) (DocumentKind, error) {
	switch k := DocumentKind(s); k {
	case DocumentRegistrationCertificate, DocumentCharter, DocumentBankConfirmation, DocumentIdentity:
		return k, nil
	default:
		return "", ErrInvalidDocumentKind.WithDetail("%q", s)
	}
}

type Document struct {
	kind       DocumentKind
	objectKey  string
	uploadedAt time.Time
}

func NewDocument(kind, objectKey string, uploadedAt time.Time) (Document, error) {
	k, err := ParseDocumentKind(kind)
	if err != nil {
		return Document{}, err
	}
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" || len(objectKey) > 512 || strings.Contains(objectKey, "..") || strings.HasPrefix(objectKey, "/") {
		return Document{}, ErrInvalidDocumentKey
	}
	return Document{kind: k, objectKey: objectKey, uploadedAt: uploadedAt}, nil
}

func (d Document) Kind() DocumentKind { return d.kind }

func (d Document) ObjectKey() string { return d.objectKey }

func (d Document) UploadedAt() time.Time { return d.uploadedAt }

type MemberRole string

const (
	MemberAdmin    MemberRole = "seller_admin"
	MemberOperator MemberRole = "seller_operator"
)

func ParseMemberRole(s string) (MemberRole, error) {
	switch r := MemberRole(s); r {
	case MemberAdmin, MemberOperator:
		return r, nil
	default:
		return "", ErrInvalidMemberRole.WithDetail("%q", s)
	}
}

type Member struct {
	userID  kernel.UserID
	role    MemberRole
	addedAt time.Time
}

func (m Member) UserID() kernel.UserID { return m.userID }

func (m Member) Role() MemberRole { return m.role }

func (m Member) AddedAt() time.Time { return m.addedAt }

type SuspensionReason string

const (
	SuspendedManually    SuspensionReason = "manual"
	SuspendedByLowRating SuspensionReason = "low_rating"
)

type categoryTag struct{}

type CategoryID = kernel.ID[categoryTag]

func ParseCategoryID(s string) (CategoryID, error) { return kernel.ParseID[categoryTag](s) }

func NewCategoryID() CategoryID { return kernel.NewID[categoryTag]() }
