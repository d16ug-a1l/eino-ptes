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
	Phase           string            `json:"phase"`
	OpenPorts       []OpenPort        `json:"open_ports,omitempty"`
	Risks           []Risk            `json:"risks,omitempty"`
	Recommendations []string          `json:"recommendations,omitempty"`
	Vulnerabilities []Vulnerability   `json:"vulnerabilities,omitempty"`
	Summary         string            `json:"summary"`
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
type ScanAnalyzer interface {
	Analyze(ctx context.Context, phase string, rawOutput string) (*ScanAnalysis, error)
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
func (a *LLMScanAnalyzer) Analyze(ctx context.Context, phase string, rawOutput string) (*ScanAnalysis, error) {
	prompt := buildAnalysisPrompt(phase, rawOutput)

	msgs := []*schema.Message{
		schema.SystemMessage("You are a cybersecurity analysis expert. Respond strictly in JSON format without markdown code blocks."),
		schema.UserMessage(prompt),
	}

	resp, err := a.model.Generate(ctx, msgs)
	if err != nil {
		return nil, fmt.Errorf("model generate: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	// Strip markdown code fences if present
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

func buildAnalysisPrompt(phase, rawOutput string) string {
	var sb strings.Builder
	sb.WriteString("Analyze the following scan output and return a JSON object. ")

	switch phase {
	case "reconnaissance":
		sb.WriteString(`
The output is from an nmap network scan. Extract:
1. open_ports: array of {port, protocol, service, version}
2. risks: array of {level, description} where level is "high", "medium", or "low"
3. recommendations: array of strings for next steps
4. summary: a brief overall summary

JSON schema:
{
  "open_ports": [...],
  "risks": [...],
  "recommendations": [...],
  "summary": "..."
}`)
	case "vulnerability_scan":
		sb.WriteString(`
The output is from a vulnerability scanner (nikto or similar). Extract:
1. vulnerabilities: array of {name, severity, description, url}
2. risks: array of {level, description}
3. recommendations: array of strings for remediation
4. summary: a brief overall summary

JSON schema:
{
  "vulnerabilities": [...],
  "risks": [...],
  "recommendations": [...],
  "summary": "..."
}`)
	default:
		sb.WriteString(`
Extract:
1. risks: array of {level, description}
2. recommendations: array of strings
3. summary: a brief overall summary

JSON schema:
{
  "risks": [...],
  "recommendations": [...],
  "summary": "..."
}`)
	}

	sb.WriteString("\n\nScan output:\n```\n")
	sb.WriteString(rawOutput)
	sb.WriteString("\n```\n")
	return sb.String()
}

func stripMarkdownFences(s string) string {
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// NoopScanAnalyzer is a fallback analyzer that does nothing.
type NoopScanAnalyzer struct{}

func (NoopScanAnalyzer) Analyze(_ context.Context, phase string, rawOutput string) (*ScanAnalysis, error) {
	return &ScanAnalysis{
		Phase:   phase,
		Summary: "LLM analysis not configured.",
	}, nil
}
