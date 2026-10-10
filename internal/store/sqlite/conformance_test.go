package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sadeq-n-yazdi/todo-bot/internal/store"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store/sqlite"
	"github.com/sadeq-n-yazdi/todo-bot/internal/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) func() store.Store {
		path := filepath.Join(t.TempDir(), "t.db")
		return func() store.Store {
			d, err := sqlite.Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			return d
		}
	})
}
