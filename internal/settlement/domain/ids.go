package domain

import "github.com/gliedabrennung/go-marketplace-backend/internal/shared/kernel"

type entryTag struct{}

type EntryID = kernel.ID[entryTag]

func NewEntryID() EntryID { return kernel.NewID[entryTag]() }

func ParseEntryID(s string) (EntryID, error) { return kernel.ParseID[entryTag](s) }
