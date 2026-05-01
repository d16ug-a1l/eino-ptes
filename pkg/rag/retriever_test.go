package rag

import (
	"context"
	"testing"
)

func TestMemoryRetriever(t *testing.T) {
	idx := NewMemoryIndexer()
	InitKnowledgeBase(idx)

	ret := NewMemoryRetriever(idx)
	ctx := context.Background()

	docs, err := ret.Retrieve(ctx, "nmap scan network")
	if err != nil {
		t.Fatalf("retrieve error: %v", err)
	}

	found := false
	for _, doc := range docs {
		if doc.ID == "nmap-basics" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find nmap-basics document")
	}
}

func TestMemoryIndexerStoreAndLoad(t *testing.T) {
	idx := NewMemoryIndexer()
	ctx := context.Background()

	err := idx.Store(ctx, nil)
	if err != nil {
		t.Fatalf("store error: %v", err)
	}

	docs, err := idx.Load(ctx)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 docs, got %d", len(docs))
	}
}
