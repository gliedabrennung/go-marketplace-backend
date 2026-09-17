//go:build integration

package search_test

import (
	"os"
	"testing"

	"github.com/gliedabrennung/go-marketplace-backend/test/testdb"
)

func TestMain(m *testing.M) {
	os.Exit(testdb.Main(m))
}
