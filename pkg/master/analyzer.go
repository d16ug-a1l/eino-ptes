package master

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// ScanAnalysis holds the structured analysis result for a single scan phase.
type ScanAnalysis struct {
	Phase           string          `json:"phase"`
	OpenPorts       []OpenPort      `json:"open_ports,omitempty"`
	Risks           []Risk          `json:"risks,omitempty"`
	Recommendations []string        `json:"recommendations,omitempty"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities,omitempty"`
	Summary         string          `json:"summary"`
}

type OpenPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Service  string `json:"service"`
	Version  string `json:"version,omitempty"`
}

type Risk struct {
	Level       string `json:"level"` // high / medium / low
	Description string `json:"description"`
}

type Vulnerability struct {
	Name        string `json:"name"`
	Severity    string `json:"severity"` // critical / high / medium / low / info
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
}

// ScanAnalyzer analyzes raw scan output and produces structured findings.
// Context (previous phase analyses) can be passed to enable coherent multi-phase reasoning.
type ScanAnalyzer interface {
	Analyze(ctx context.Context, phase string, rawOutput string, contextAnalyses map[string]*ScanAnalysis) (*ScanAnalysis, error)
}

// LLMScanAnalyzer uses a language model to analyze scan results.
type LLMScanAnalyzer struct {
	model model.BaseChatModel
}

// NewLLMScanAnalyzer creates an analyzer backed by the given chat model.
func NewLLMScanAnalyzer(m model.BaseChatModel) *LLMScanAnalyzer {
	return &LLMScanAnalyzer{model: m}
}

// Analyze sends the raw scan output to the LLM and parses the structured response.
// If contextAnalyses contains previous phase results, they are included in the prompt
// so the model can reason coherently across the full PTES pipeline.
func (a *LLMScanAnalyzer) Analyze(ctx context.Context, phase string, rawOutput string, contextAnalyses map[string]*ScanAnalysis) (*ScanAnalysis, error) {
	msgs := buildAnalysisMessages(phase, rawOutput, contextAnalyses)

	resp, err := a.model.Generate(ctx, msgs)
	if err != nil {
		return nil, fmt.Errorf("model generate: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	content = stripMarkdownFences(content)

	var analysis ScanAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		// If JSON parsing fails, fall back to a simple summary
		return &ScanAnalysis{
			Phase:   phase,
			Summary: content,
		}, nil
	}
	analysis.Phase = phase
	return &analysis, nil
}

func buildAnalysisMessages(phase, rawOutput string, contextAnalyses map[string]*ScanAnalysis) []*schema.Message {
	// System instruction: role + output format constraints
	systemContent := `You are a senior penetration testing analyst. Your job is to analyze raw security scanner output and produce structured findings in JSON.

Rules:
- Respond ONLY with a JSON object. Do not wrap it in markdown code blocks.
- Severity/level fields must be one of: critical, high, medium, low, info.
- Be concise but thorough. Focus on actionable findings.`

	// Build user message with context + task + raw output
	var userParts []string
	userParts = append(userParts, fmt.Sprintf("Current phase: %s", phase))

	// Inject previous phase analyses as context (memory)
	if len(contextAnalyses) > 0 {
		userParts = append(userParts, "\nPrevious phase findings (for context):")
		for prevPhase, analysis := range contextAnalyses {
			if analysis == nil {
				continue
			}
			b, _ := json.Marshal(analysis)
			userParts = append(userParts, fmt.Sprintf("- %s: %s", prevPhase, string(b)))
		}
	}

	// Phase-specific extraction instructions
	userParts = append(userParts, "\nExtraction requirements:")
	switch phase {
	case "reconnaissance":
		userParts = append(userParts, `The output is from an nmap network scan. Extract:
1. open_ports: array of {port (int), protocol (string), service (string), version (string)}
2. risks: array of {level ("high"|"medium"|"low"), description (string)}
3. recommendations: array of strings for next penetration testing steps
4. summary: a brief overall summary

Expected JSON schema:
{
  "open_ports": [...],
  "risks": [...],
  "recommendations": [...],
  "summary": "..."
}`)
	case "vulnerability_scan":
		userParts = append(userParts, `The output is from a web vulnerability scanner (nikto or similar). Extract:
1. vulnerabilities: array of {name (string), severity ("critical"|"high"|"medium"|"low"|"info"), description (string), url (string, optional)}
2. risks: array of {level ("high"|"medium"|"low"), description (string)}
3. recommendations: array of strings for remediation
4. summary: a brief overall summary

Expected JSON schema:
{
  "vulnerabilities": [...],
  "risks": [...],
  "recommendations": [...],
  "summary": "..."
}`)
	default:
		userParts = append(userParts, `Extract:
1. risks: array of {level ("high"|"medium"|"low"), description (string)}
2. recommendations: array of strings
3. summary: a brief overall summary

Expected JSON schema:
{
  "risks": [...],
  "recommendations": [...],
  "summary": "..."
}`)
	}

	userParts = append(userParts, fmt.Sprintf("\nRaw scan output:\n```\n%s\n```", rawOutput))

	return []*schema.Message{
		schema.SystemMessage(systemContent),
		schema.UserMessage(strings.Join(userParts, "\n")),
	}
}

func stripMarkdownFences(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// NoopScanAnalyzer is a fallback analyzer that does nothing.
type NoopScanAnalyzer struct{}

func (NoopScanAnalyzer) Analyze(_ context.Context, phase string, rawOutput string, _ map[string]*ScanAnalysis) (*ScanAnalysis, error) {
	return &ScanAnalysis{
		Phase:   phase,
		Summary: "LLM analysis not configured.",
	}, nil
}
