package service

import (
	"errors"
	"strings"
	"testing"
)

type fakeFormatAIClient struct {
	responses []string
	errors    []error
	calls     int
}

func TestInitAIClientDisablesLegacyWholeTemplateCalls(t *testing.T) {
	parser := NewFormatParserService()
	parser.InitAIClient("configured-cookie", "configured-bearer", true)
	if parser.aiClient != nil {
		t.Fatal("legacy parser must not initialize a DeepSeek client for complete template text")
	}
}

func (f *fakeFormatAIClient) ChatCompletion(string) (string, error) {
	i := f.calls
	f.calls++
	if i < len(f.errors) && f.errors[i] != nil {
		return "", f.errors[i]
	}
	if i >= len(f.responses) {
		return "", errors.New("unexpected DeepSeek call")
	}
	return f.responses[i], nil
}

func TestParseFormatFromTextDetailedDoesNotSendLegacyChunks(t *testing.T) {
	client := &fakeFormatAIClient{responses: []string{
		`{"title":{"font_size":"小二"}}`,
		`{"body":{"font_name":"宋体"}}`,
		`{"title":{"font_size":"小二"}}`,
		`{"body":{"font_name":"宋体"}}`,
	}}
	parser := NewFormatParserService()
	parser.aiClient = client
	parser.SetMaxAICalls(4)

	result, err := parser.ParseFormatFromTextDetailed(strings.Repeat("format requirement ", 500))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("legacy DeepSeek calls = %d, want 0", client.calls)
	}
	if result.Quality.SuccessfulChunks != 0 || result.Quality.HighConfidence {
		t.Fatalf("quality = %+v", result.Quality)
	}
}

func TestParseFormatFromTextDetailedDoesNotRetryLegacyAI(t *testing.T) {
	client := &fakeFormatAIClient{responses: []string{
		`not json`,
		`{"title":{"font_size":"小二"}}`,
	}}
	parser := NewFormatParserService()
	parser.aiClient = client
	parser.SetMaxAICalls(2)

	result, err := parser.ParseFormatFromTextDetailed(strings.Repeat("title format ", 20))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if client.calls != 0 || result.Quality.SuccessfulChunks != 0 || result.Quality.HighConfidence {
		t.Fatalf("calls=%d quality=%+v", client.calls, result.Quality)
	}
}

func TestParseFormatFromTextDetailedRejectsUnknownTopLevelRules(t *testing.T) {
	client := &fakeFormatAIClient{responses: []string{
		`{"title":{"font_size":"小二"},"invented":{"value":"x"}}`,
		`{"title":{"font_size":"小二"},"invented":{"value":"x"}}`,
	}}
	parser := NewFormatParserService()
	parser.aiClient = client
	parser.SetMaxAICalls(2)

	result, err := parser.ParseFormatFromTextDetailed(strings.Repeat("title format ", 20))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := result.Rules["invented"]; ok {
		t.Fatalf("unknown top-level rule was not removed: %#v", result.Rules)
	}
	if result.Quality.HighConfidence || result.Quality.QualityScore >= 0.98 {
		t.Fatalf("invalid schema should lower confidence: %+v", result.Quality)
	}
}

func TestParseFormatFromTextDetailedReportsBudgetCoverage(t *testing.T) {
	client := &fakeFormatAIClient{responses: []string{
		`{"title":{"font_size":"小二"}}`,
		`{"title":{"font_size":"小二"}}`,
	}}
	parser := NewFormatParserService()
	parser.aiClient = client
	parser.SetMaxAICalls(2)

	result, err := parser.ParseFormatFromTextDetailed(strings.Repeat("format requirement ", 1000))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Quality.CoverageRate >= 1 || result.Quality.HighConfidence {
		t.Fatalf("truncated extraction must not be high confidence: %+v", result.Quality)
	}
	if len(result.Quality.Warnings) < 2 {
		t.Fatalf("expected budget and benchmark warnings: %+v", result.Quality.Warnings)
	}
}
