package rag

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/schema"
)

type MemoryIndexer struct {
	mu        sync.RWMutex
	documents map[string]*schema.Document
}

func NewMemoryIndexer() *MemoryIndexer {
	return &MemoryIndexer{documents: make(map[string]*schema.Document)}
}

func (i *MemoryIndexer) Store(ctx context.Context, docs []*schema.Document, opts ...any) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, doc := range docs {
		if doc.ID == "" {
			doc.ID = fmt.Sprintf("doc-%d", len(i.documents))
		}
		i.documents[doc.ID] = doc
	}
	return nil
}

func (i *MemoryIndexer) Load(ctx context.Context) ([]*schema.Document, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	result := make([]*schema.Document, 0, len(i.documents))
	for _, doc := range i.documents {
		result = append(result, doc)
	}
	return result, nil
}

func (i *MemoryIndexer) AddKnowledge(title, content string, tags []string) {
	doc := &schema.Document{
		ID:      title,
		Content: content,
		MetaData: map[string]any{
			"tags": tags,
		},
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.documents[doc.ID] = doc
}

func InitKnowledgeBase(idx *MemoryIndexer) {
	idx.AddKnowledge("nmap-basics",
		"nmap is a network discovery and security auditing tool. Common flags: -sS (SYN scan), -sV (version detection), -sC (script scan), -O (OS detection), -A (aggressive scan), -p (port specification).",
		[]string{"reconnaissance", "nmap"})
	idx.AddKnowledge("nikto-basics",
		"nikto is a web server scanner that tests for dangerous files/CGIs, outdated versions, and configuration issues. Common usage: nikto -h <target>.",
		[]string{"vulnerability_scan", "nikto", "web"})
	idx.AddKnowledge("information-gathering",
		"Information gathering is the first phase of penetration testing. It includes: passive reconnaissance (OSINT, whois, DNS), active reconnaissance (port scanning, service enumeration), and network mapping.",
		[]string{"reconnaissance", "methodology"})
	idx.AddKnowledge("vulnerability-verification",
		"After discovering potential vulnerabilities, verification is needed. Steps: confirm the vulnerability exists, determine exploitability, assess impact, document findings with evidence.",
		[]string{"vulnerability_scan", "methodology"})
}
