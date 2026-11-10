package keystore

import (
	"context"
	"errors"
	"testing"

	"github.com/pwagstro/simple_llm_proxy/internal/storage"
)

type flushStore struct {
	storage.Storage // unimplemented methods panic if called
	totals          map[int64]float64
	flushes         []int64
	fail            bool
}

func (f *flushStore) GetKeySpendTotals(context.Context) (map[int64]float64, error) {
	return f.totals, nil
}

func (f *flushStore) FlushKeySpend(_ context.Context, keyID int64, _ float64) error {
	if f.fail {
		return errors.New("disk full")
	}
	f.flushes = append(f.flushes, keyID)
	return nil
}

func TestFlushWritesOnlyChangedKeys(t *testing.T) {
	ctx := context.Background()
	store := &flushStore{totals: map[int64]float64{1: 2.5, 2: 4}}
	sa := NewSpendAccumulator()
	if err := sa.InitFromStorage(ctx, store); err != nil {
		t.Fatal(err)
	}
	if err := sa.FlushToStorage(ctx, store); err != nil || len(store.flushes) != 0 {
		t.Fatalf("restored totals should not be re-flushed: %v %v", store.flushes, err)
	}
	sa.Credit(1, 0.5)
	sa.Credit(3, 1) // first spend for a new key
	if err := sa.FlushToStorage(ctx, store); err != nil {
		t.Fatal(err)
	}
	if got := map[int64]bool{}; len(store.flushes) != 2 {
		t.Fatalf("flushes = %v", store.flushes)
	} else {
		for _, id := range store.flushes {
			got[id] = true
		}
		if !got[1] || !got[3] {
			t.Fatalf("flushes = %v, want keys 1 and 3", store.flushes)
		}
	}
	store.flushes = nil
	for i := 0; i < 3; i++ { // idle ticks
		if err := sa.FlushToStorage(ctx, store); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.flushes) != 0 {
		t.Fatalf("idle flushes wrote rows: %v", store.flushes)
	}
}

func TestFailedFlushIsRetried(t *testing.T) {
	ctx := context.Background()
	store := &flushStore{fail: true}
	sa := NewSpendAccumulator()
	sa.Credit(7, 1)
	if err := sa.FlushToStorage(ctx, store); err == nil {
		t.Fatal("expected flush error")
	}
	store.fail = false
	if err := sa.FlushToStorage(ctx, store); err != nil || len(store.flushes) != 1 || store.flushes[0] != 7 {
		t.Fatalf("retry flushes = %v err=%v", store.flushes, err)
	}
}
