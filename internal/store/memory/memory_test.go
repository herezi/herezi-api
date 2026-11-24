package memory_test

import (
	"testing"

	"github.com/herezi/herezi-api/internal/store"
	"github.com/herezi/herezi-api/internal/store/memory"
	"github.com/herezi/herezi-api/internal/store/storetest"
)

func TestStore(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.SpotStore { return memory.New() })
}

func TestSaved(t *testing.T) {
	storetest.RunSaved(t, func(*testing.T) storetest.SavedSpotStore { return memory.New() })
}
