package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gliedabrennung/go-marketplace-backend/internal/catalog/domain"
)

func TestNewAttributeDefinition_Valid(t *testing.T) {
	def, err := domain.NewAttributeDefinition(domain.AttributeSpec{
		Code: "color", Name: " Цвет ", Type: "enum", Required: true, Filterable: true, Options: []string{" black ", "white"},
	})
	require.NoError(t, err)
	assert.Equal(t, "color", def.Code())
	assert.Equal(t, "Цвет", def.Name())
	assert.Equal(t, domain.AttributeEnum, def.Type())
	assert.True(t, def.Required())
	assert.True(t, def.Filterable())
	assert.Equal(t, []string{"black", "white"}, def.Options())
	assert.Empty(t, def.Unit())

	restored, err := domain.NewAttributeDefinition(def.Spec())
	require.NoError(t, err)
	assert.Equal(t, def, restored)
}

func TestNewAttributeDefinition_Invalid(t *testing.T) {
	cases := map[string]struct {
		spec domain.AttributeSpec
		err  error
	}{
		"code uppercase":     {domain.AttributeSpec{Code: "Color", Name: "n", Type: "string"}, domain.ErrInvalidAttributeCode},
		"code digit first":   {domain.AttributeSpec{Code: "1color", Name: "n", Type: "string"}, domain.ErrInvalidAttributeCode},
		"code too long":      {domain.AttributeSpec{Code: "a" + strings.Repeat("b", 64), Name: "n", Type: "string"}, domain.ErrInvalidAttributeCode},
		"code dash":          {domain.AttributeSpec{Code: "my-code", Name: "n", Type: "string"}, domain.ErrInvalidAttributeCode},
		"empty name":         {domain.AttributeSpec{Code: "a", Name: " ", Type: "string"}, domain.ErrInvalidAttributeName},
		"unknown type":       {domain.AttributeSpec{Code: "a", Name: "n", Type: "date"}, domain.ErrInvalidAttributeType},
		"enum without opts":  {domain.AttributeSpec{Code: "a", Name: "n", Type: "enum"}, domain.ErrInvalidAttributeOptions},
		"enum duplicate opt": {domain.AttributeSpec{Code: "a", Name: "n", Type: "enum", Options: []string{"x", " x"}}, domain.ErrInvalidAttributeOptions},
		"enum empty opt":     {domain.AttributeSpec{Code: "a", Name: "n", Type: "enum", Options: []string{""}}, domain.ErrInvalidAttributeOptions},
		"string with opts":   {domain.AttributeSpec{Code: "a", Name: "n", Type: "string", Options: []string{"x"}}, domain.ErrInvalidAttributeOptions},
		"unit without unit":  {domain.AttributeSpec{Code: "a", Name: "n", Type: "unit"}, domain.ErrInvalidAttributeUnit},
		"number with unit":   {domain.AttributeSpec{Code: "a", Name: "n", Type: "number", Unit: "kg"}, domain.ErrInvalidAttributeUnit},
	}
	for name, tc := range cases {
		_, err := domain.NewAttributeDefinition(tc.spec)
		require.ErrorIs(t, err, tc.err, name)
	}
}

func TestAttributeDefinition_Parse(t *testing.T) {
	number := attr(t, domain.AttributeSpec{Code: "weight", Name: "Вес", Type: "unit", Unit: "kg"})
	v, err := number.Parse(" 1.50 ")
	require.NoError(t, err)
	n, ok := v.Number()
	assert.True(t, ok)
	assert.InDelta(t, 1.5, n, 0)
	assert.Equal(t, "1.5", v.String())
	assert.Equal(t, domain.AttributeUnit, v.Type())
	for _, raw := range []string{"abc", "NaN", "Inf", ""} {
		_, err := number.Parse(raw)
		require.ErrorIs(t, err, domain.ErrInvalidAttributes, raw)
	}

	boolean := attr(t, domain.AttributeSpec{Code: "nfc", Name: "NFC", Type: "boolean"})
	v, err = boolean.Parse("true")
	require.NoError(t, err)
	assert.Equal(t, "true", v.String())
	_, ok = v.Number()
	assert.False(t, ok)
	_, err = boolean.Parse("yes")
	require.ErrorIs(t, err, domain.ErrInvalidAttributes)

	enum := attr(t, domain.AttributeSpec{Code: "color", Name: "Цвет", Type: "enum", Options: []string{"red"}})
	v, err = enum.Parse("red")
	require.NoError(t, err)
	assert.Equal(t, "red", v.String())
	_, err = enum.Parse("Red")
	require.ErrorIs(t, err, domain.ErrInvalidAttributes)

	text := attr(t, domain.AttributeSpec{Code: "model", Name: "Модель", Type: "string"})
	v, err = text.Parse("  X1 ")
	require.NoError(t, err)
	assert.Equal(t, "X1", v.String())
	assert.False(t, v.IsZero())
	_, err = text.Parse(strings.Repeat("я", 1001))
	require.ErrorIs(t, err, domain.ErrInvalidAttributes)

	var zero domain.AttributeValue
	assert.True(t, zero.IsZero())
}
