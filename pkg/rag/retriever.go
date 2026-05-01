package rag

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/schema"
)

type MemoryRetriever struct {
	indexer *MemoryIndexer
}

func NewMemoryRetriever(indexer *MemoryIndexer) *MemoryRetriever {
	return &MemoryRetriever{indexer: indexer}
}

func (r *MemoryRetriever) Retrieve(ctx context.Context, query string, opts ...any) ([]*schema.Document, error) {
	docs, err := r.indexer.Load(ctx)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	var results []*schema.Document
	for _, doc := range docs {
		score := matchScore(queryLower, doc)
		if score > 0 {
			results = append(results, doc)
		}
	}
	return results, nil
}

func matchScore(query string, doc *schema.Document) int {
	score := 0
	content := strings.ToLower(doc.Content)
	title := strings.ToLower(doc.ID)

	queryWords := strings.Fields(query)
	for _, word := range queryWords {
		if strings.Contains(title, word) {
			score += 3
		}
		if strings.Contains(content, word) {
			score += 1
		}
	}

	if tags, ok := doc.MetaData["tags"].([]string); ok {
		for _, tag := range tags {
			if strings.Contains(query, strings.ToLower(tag)) {
				score += 2
			}
		}
	}

	return score
}
