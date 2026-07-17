package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// FormatParseQuality reports extraction coverage and parser health. It is not
// a substitute for accuracy measured against a labelled document set.
type FormatParseQuality struct {
	AIUsed           bool     `json:"ai_used"`
	QualityScore     float64  `json:"quality_score"`
	HighConfidence   bool     `json:"high_confidence"`
	SourceRunes      int      `json:"source_runes"`
	CoveredRunes     int      `json:"covered_runes"`
	CoverageRate     float64  `json:"coverage_rate"`
	ChunkCount       int      `json:"chunk_count"`
	SuccessfulChunks int      `json:"successful_chunks"`
	VerifiedChunks   int      `json:"verified_chunks"`
	RetriedChunks    int      `json:"retried_chunks"`
	Warnings         []string `json:"warnings,omitempty"`
}

type FormatParseResult struct {
	Rules   map[string]interface{} `json:"rules"`
	Quality FormatParseQuality     `json:"quality"`
}

var allowedFormatRuleKeys = map[string]struct{}{
	"page_setup": {}, "title": {}, "author": {}, "abstract": {},
	"keywords": {}, "english_title": {}, "english_abstract": {},
	"english_keywords": {}, "body": {}, "headings": {}, "references": {},
	"table_of_contents": {}, "figures": {}, "tables": {}, "footnotes": {},
	"cover": {}, "acknowledgements": {}, "appendix": {},
}

func splitFormatText(text string, maxRunes, maxChunks int) ([]string, int, bool) {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 || maxRunes <= 0 || maxChunks <= 0 {
		return nil, 0, len(runes) > 0
	}

	capacity := (len(runes) + maxRunes - 1) / maxRunes
	if capacity > maxChunks {
		capacity = maxChunks
	}
	chunks := make([]string, 0, capacity)
	covered := 0
	for start := 0; start < len(runes) && len(chunks) < maxChunks; start += maxRunes {
		end := start + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		covered = end
	}
	return chunks, covered, covered < len(runes)
}

func sanitizeFormatRules(rules map[string]interface{}) (map[string]interface{}, float64) {
	if len(rules) == 0 {
		return map[string]interface{}{}, 0
	}
	clean := make(map[string]interface{}, len(rules))
	valid := 0
	for key, value := range rules {
		if _, ok := allowedFormatRuleKeys[key]; !ok {
			continue
		}
		valid++
		clean[key] = value
	}
	return clean, float64(valid) / float64(len(rules))
}

func roundQuality(value float64) float64 {
	return float64(int(value*10000+0.5)) / 10000
}

func (s *FormatParserService) reviewFormatChunk(chunk string, candidate map[string]interface{}) (map[string]interface{}, error) {
	candidateJSON, err := json.Marshal(candidate)
	if err != nil {
		return nil, err
	}
	prompt := fmt.Sprintf(`You are verifying extracted thesis formatting rules.
Compare the candidate JSON against the source text. Remove every unsupported
field, correct contradictions, and add only requirements explicitly present in
the source. Keep the same schema and output JSON only.

SOURCE:
%s

CANDIDATE JSON:
%s`, chunk, candidateJSON)
	response, err := s.aiClient.ChatCompletion(prompt)
	if err != nil {
		return nil, err
	}
	fixed := fixJSONObjectBody(trimDeepSeekResponseBody(response), "[format review]")
	var rules map[string]interface{}
	if err := json.Unmarshal([]byte(fixed), &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// ParseFormatFromTextDetailed combines local extraction with chunked DeepSeek
// extraction. Each failed chunk is retried once within the configured budget.
func (s *FormatParserService) ParseFormatFromTextDetailed(text string) (FormatParseResult, error) {
	regexJSON, err := s.ParseFormatFromText(text)
	if err != nil {
		return FormatParseResult{}, fmt.Errorf("local format parsing failed: %w", err)
	}
	var regexRules map[string]interface{}
	if err := json.Unmarshal([]byte(regexJSON), &regexRules); err != nil {
		return FormatParseResult{}, fmt.Errorf("decode local format rules: %w", err)
	}

	cleaned := s.CleanTextForAI(text)
	sourceRunes := len([]rune(strings.TrimSpace(cleaned)))
	quality := FormatParseQuality{SourceRunes: sourceRunes}
	if s.aiClient == nil {
		quality.QualityScore = 0.6
		quality.Warnings = []string{"DeepSeek is unavailable; only local extraction was used"}
		return FormatParseResult{Rules: regexRules, Quality: quality}, nil
	}

	maxCalls := s.maxAICalls
	if maxCalls <= 0 {
		maxCalls = 20
	}
	maxChunks := maxCalls / 2
	if maxChunks < 1 {
		maxChunks = 1
	}
	chunks, coveredRunes, truncated := splitFormatText(cleaned, 6000, maxChunks)
	quality.AIUsed = true
	quality.CoveredRunes = coveredRunes
	quality.ChunkCount = len(chunks)
	if sourceRunes > 0 {
		quality.CoverageRate = roundQuality(float64(coveredRunes) / float64(sourceRunes))
	}
	if truncated {
		quality.Warnings = append(quality.Warnings, "The document exceeded the DeepSeek call budget; uncovered text used local extraction only")
	}

	aiRules := make(map[string]interface{})
	schemaTotal := 0.0
	type successfulChunk struct {
		text  string
		rules map[string]interface{}
	}
	failed := make([]string, 0)
	successful := make([]successfulChunk, 0, len(chunks))
	calls := 0
	mergeChunk := func(chunkRules map[string]interface{}) bool {
		cleanRules, schemaScore := sanitizeFormatRules(chunkRules)
		if len(cleanRules) == 0 {
			return false
		}
		schemaTotal += schemaScore
		aiRules = MergeFormatRules(cleanRules, aiRules)
		quality.SuccessfulChunks++
		return true
	}

	for _, chunk := range chunks {
		chunkRules, _, parseErr := s.ParseFormatWithAI(chunk, FormatAIPromptKindFormatRules)
		calls++
		if parseErr != nil {
			failed = append(failed, chunk)
			continue
		}
		cleanRules, _ := sanitizeFormatRules(chunkRules)
		if !mergeChunk(chunkRules) {
			failed = append(failed, chunk)
			continue
		}
		successful = append(successful, successfulChunk{text: chunk, rules: cleanRules})
	}
	for _, chunk := range failed {
		if calls >= maxCalls {
			break
		}
		quality.RetriedChunks++
		chunkRules, _, parseErr := s.ParseFormatWithAI(chunk, FormatAIPromptKindFormatRules)
		calls++
		if parseErr == nil && mergeChunk(chunkRules) {
			quality.VerifiedChunks++
		}
	}
	for _, item := range successful {
		if calls >= maxCalls {
			break
		}
		reviewed, reviewErr := s.reviewFormatChunk(item.text, item.rules)
		calls++
		if reviewErr != nil {
			continue
		}
		cleanRules, _ := sanitizeFormatRules(reviewed)
		if len(cleanRules) > 0 {
			aiRules = MergeFormatRules(cleanRules, aiRules)
			quality.VerifiedChunks++
		}
	}
	if quality.SuccessfulChunks < quality.ChunkCount {
		quality.Warnings = append(quality.Warnings, "One or more DeepSeek chunks failed; local extraction filled the gaps")
	}
	if quality.VerifiedChunks < quality.SuccessfulChunks {
		quality.Warnings = append(quality.Warnings, "One or more DeepSeek chunks could not complete second-pass verification")
	}

	successRate := 0.0
	schemaScore := 0.0
	verificationRate := 0.0
	if quality.ChunkCount > 0 {
		successRate = float64(quality.SuccessfulChunks) / float64(quality.ChunkCount)
	}
	if quality.SuccessfulChunks > 0 {
		schemaScore = schemaTotal / float64(quality.SuccessfulChunks)
		verificationRate = float64(quality.VerifiedChunks) / float64(quality.SuccessfulChunks)
	}
	quality.QualityScore = roundQuality(0.35*quality.CoverageRate + 0.30*successRate + 0.20*verificationRate + 0.15*schemaScore)
	quality.HighConfidence = quality.QualityScore >= 0.98
	quality.Warnings = append(quality.Warnings, "quality_score measures extraction coverage and parser health; 98% factual accuracy requires a labelled benchmark")

	merged := MergeFormatRules(aiRules, regexRules)
	log.Printf("[format parser] DeepSeek chunks=%d successful=%d verified=%d retried=%d coverage=%.4f quality=%.4f",
		quality.ChunkCount, quality.SuccessfulChunks, quality.VerifiedChunks, quality.RetriedChunks, quality.CoverageRate, quality.QualityScore)
	return FormatParseResult{Rules: merged, Quality: quality}, nil
}
