package kernel_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

var errSample = kernel.NotFound("SAMPLE_NOT_FOUND", "sample not found")

func TestError_IsSurvivesWrappingAndDetail(t *testing.T) {
	err := fmt.Errorf("load: %w", errSample.WithDetail("id=%s", "42"))
	assert.ErrorIs(t, err, errSample)
	assert.Equal(t, kernel.KindNotFound, kernel.KindOf(err))
	assert.Equal(t, "SAMPLE_NOT_FOUND", kernel.CodeOf(err))
	assert.Equal(t, "load: sample not found: id=42", err.Error())
}

func TestError_DifferentCodesAreNotEqual(t *testing.T) {
	other := kernel.NotFound("OTHER", "sample not found")
	assert.NotErrorIs(t, other, errSample)
}

func TestError_WithFieldsDoesNotMutateOriginal(t *testing.T) {
	base := kernel.Validation("BAD", "bad input")
	withFields := base.WithFields(kernel.FieldViolation{Field: "email", Code: "INVALID"})
	assert.Empty(t, base.Fields())
	assert.Len(t, withFields.Fields(), 1)
	assert.ErrorIs(t, withFields, base)
}

func TestKindOf_UnknownErrorIsInternal(t *testing.T) {
	assert.Equal(t, kernel.KindInternal, kernel.KindOf(errors.New("boom")))
	assert.Equal(t, "INTERNAL", kernel.CodeOf(errors.New("boom")))
}

func TestEventBuffer_PullClears(t *testing.T) {
	var b kernel.EventBuffer
	b.Record(nil)
	assert.Len(t, b.Peek(), 1)
	assert.Len(t, b.Pull(), 1)
	assert.Empty(t, b.Pull())
}
