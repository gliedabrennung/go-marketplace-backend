package domain

import (
	"maps"
	"slices"

	"github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"
)

type Schema struct {
	definitions []AttributeDefinition
}

func NewSchema(defs ...AttributeDefinition) (Schema, error) {
	seen := make(map[string]struct{}, len(defs))
	for _, d := range defs {
		if _, dup := seen[d.code]; dup {
			return Schema{}, ErrDuplicateAttribute.WithDetail("%q", d.code)
		}
		seen[d.code] = struct{}{}
	}
	return Schema{definitions: slices.Clone(defs)}, nil
}

func (s Schema) Definitions() []AttributeDefinition { return slices.Clone(s.definitions) }

func (s Schema) Definition(code string) (AttributeDefinition, bool) {
	for _, d := range s.definitions {
		if d.code == code {
			return d, true
		}
	}
	return AttributeDefinition{}, false
}

func (s Schema) Has(code string) bool {
	_, ok := s.Definition(code)
	return ok
}

func (s Schema) ParseValues(raw map[string]string) (map[string]AttributeValue, error) {
	values := make(map[string]AttributeValue, len(raw))
	var violations []kernel.FieldViolation
	for _, code := range slices.Sorted(maps.Keys(raw)) {
		def, ok := s.Definition(code)
		if !ok {
			violations = append(violations, kernel.FieldViolation{
				Field: "attributes." + code, Code: "UNKNOWN_ATTRIBUTE", Message: "attribute is not defined for the category",
			})
			continue
		}
		v, violation := def.parse(raw[code])
		if violation != nil {
			violations = append(violations, *violation)
			continue
		}
		values[code] = v
	}
	if len(violations) > 0 {
		return nil, ErrInvalidAttributes.WithFields(violations...)
	}
	return values, nil
}

func (s Schema) MissingRequired(values map[string]AttributeValue) []kernel.FieldViolation {
	var out []kernel.FieldViolation
	for _, d := range s.definitions {
		if _, ok := values[d.code]; d.required && !ok {
			out = append(out, kernel.FieldViolation{Field: "attributes." + d.code, Code: "REQUIRED", Message: d.name + " is required"})
		}
	}
	return out
}

type AttributeEntry struct {
	Definition AttributeDefinition
	Value      AttributeValue
}

func (s Schema) Entries(values map[string]AttributeValue) []AttributeEntry {
	out := make([]AttributeEntry, 0, len(values))
	for _, d := range s.definitions {
		if v, ok := values[d.code]; ok {
			out = append(out, AttributeEntry{Definition: d, Value: v})
		}
	}
	return out
}

type Classification struct {
	categoryID CategoryID
	path       []CategoryID
	schema     Schema
}

func Classify(chain []*Category) (Classification, error) {
	if len(chain) == 0 {
		return Classification{}, ErrCategoryNotFound
	}
	var (
		defs     []AttributeDefinition
		expected []CategoryID
	)
	for _, c := range chain {
		if !slices.Equal(c.ancestors, expected) {
			return Classification{}, ErrBrokenCategoryChain
		}
		defs = append(defs, c.attributes...)
		expected = append(expected, c.id)
	}
	schema, err := NewSchema(defs...)
	if err != nil {
		return Classification{}, err
	}
	leaf := chain[len(chain)-1]
	return Classification{categoryID: leaf.id, path: leaf.Path(), schema: schema}, nil
}

func (c Classification) CategoryID() CategoryID { return c.categoryID }

func (c Classification) Path() []CategoryID { return slices.Clone(c.path) }

func (c Classification) Schema() Schema { return c.schema }
