package bm25

import (
	"context"
	"fmt"
	"sync"
)

// Index is the process-local default retrieval adapter. T2 will add persisted
// question loading and scoped search while keeping this boundary unchanged.
type Index struct {
	mu     sync.RWMutex
	closed bool
}

func New() *Index {
	return &Index{}
}

func (i *Index) Check(context.Context) error {
	if i == nil {
		return fmt.Errorf("bm25: index is nil")
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.closed {
		return fmt.Errorf("bm25: index is closed")
	}
	return nil
}

func (i *Index) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
}
