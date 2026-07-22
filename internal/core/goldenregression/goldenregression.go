package goldenregression

import (
	"fmt"
	"strings"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type Issue struct {
	Kind     string   `json:"kind"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Target   string   `json:"target"`
}

type PageSnapshot struct {
	Pages []string   `json:"pages"`
	Spans []TextSpan `json:"spans,omitempty"`
}

type TextSpan struct {
	Page     int     `json:"page"`
	Text     string  `json:"text"`
	Font     string  `json:"font,omitempty"`
	FontSize float64 `json:"font_size,omitempty"`
	X        float64 `json:"x,omitempty"`
}

type Landmark struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type SamePageLandmark struct {
	Name  string `json:"name"`
	Left  string `json:"left"`
	Right string `json:"right"`
}

type Options struct {
	Candidate        PageSnapshot
	Golden           PageSnapshot
	CheckPageCount   bool
	MaxPageDelta     int
	Landmarks        []Landmark
	SamePageLandmark []SamePageLandmark
	CompareStyles    bool
}

type Result struct {
	Passed             bool    `json:"passed"`
	CandidatePageCount int     `json:"candidate_page_count"`
	GoldenPageCount    int     `json:"golden_page_count"`
	Issues             []Issue `json:"issues,omitempty"`
}

func CompareSnapshots(options Options) Result {
	result := Result{
		CandidatePageCount: len(options.Candidate.Pages),
		GoldenPageCount:    len(options.Golden.Pages),
	}
	maxDelta := options.MaxPageDelta
	if maxDelta < 0 {
		maxDelta = 0
	}
	pageDelta := result.CandidatePageCount - result.GoldenPageCount
	if pageDelta < 0 {
		pageDelta = -pageDelta
	}
	if options.CheckPageCount && result.GoldenPageCount > 0 && pageDelta > maxDelta {
		result.Issues = append(result.Issues, Issue{
			Kind:     "page_count_drift",
			Severity: SeverityError,
			Message:  fmt.Sprintf("candidate page count drifted by %d pages from golden sample", pageDelta),
			Target:   "document",
		})
	}

	for _, landmark := range options.Landmarks {
		goldenPage := findPage(options.Golden.Pages, landmark.Text)
		candidatePage := findPage(options.Candidate.Pages, landmark.Text)
		if goldenPage == 0 {
			result.Issues = append(result.Issues, Issue{
				Kind:     "golden_landmark_missing",
				Severity: SeverityWarning,
				Message:  "golden sample does not contain configured landmark",
				Target:   landmark.Name,
			})
			continue
		}
		if candidatePage == 0 {
			result.Issues = append(result.Issues, Issue{
				Kind:     "candidate_landmark_missing",
				Severity: SeverityError,
				Message:  "candidate output does not contain required golden landmark",
				Target:   landmark.Name,
			})
			continue
		}
		if candidatePage != goldenPage {
			result.Issues = append(result.Issues, Issue{
				Kind:     "landmark_page_drift",
				Severity: SeverityError,
				Message:  fmt.Sprintf("landmark %q moved from golden page %d to candidate page %d", landmark.Name, goldenPage, candidatePage),
				Target:   landmark.Name,
			})
		}
		if options.CompareStyles {
			compareLandmarkStyle(&result, landmark, options.Candidate.Spans, options.Golden.Spans)
		}
	}

	for _, pair := range options.SamePageLandmark {
		goldenLeft := findPage(options.Golden.Pages, pair.Left)
		goldenRight := findPage(options.Golden.Pages, pair.Right)
		candidateLeft := findPage(options.Candidate.Pages, pair.Left)
		candidateRight := findPage(options.Candidate.Pages, pair.Right)
		if goldenLeft != 0 && goldenRight != 0 && goldenLeft == goldenRight {
			if candidateLeft == 0 || candidateRight == 0 {
				result.Issues = append(result.Issues, Issue{
					Kind:     "candidate_same_page_landmark_missing",
					Severity: SeverityError,
					Message:  "candidate output cannot prove golden same-page landmark rule",
					Target:   pair.Name,
				})
				continue
			}
			if candidateLeft != candidateRight {
				result.Issues = append(result.Issues, Issue{
					Kind:     "candidate_same_page_drift",
					Severity: SeverityError,
					Message:  fmt.Sprintf("golden keeps %q landmarks on one page, candidate places them on page %d and %d", pair.Name, candidateLeft, candidateRight),
					Target:   pair.Name,
				})
			}
		}
	}

	result.Passed = !hasErrors(result.Issues)
	return result
}

func compareLandmarkStyle(result *Result, landmark Landmark, candidate, golden []TextSpan) {
	candidateSpan, candidateOK := findSpan(candidate, landmark.Text)
	goldenSpan, goldenOK := findSpan(golden, landmark.Text)
	if !candidateOK || !goldenOK {
		result.Issues = append(result.Issues, Issue{Kind: "landmark_style_unavailable", Severity: SeverityWarning, Message: "rendered font metadata is unavailable for landmark", Target: landmark.Name})
		return
	}
	if normalizeFont(candidateSpan.Font) != normalizeFont(goldenSpan.Font) {
		result.Issues = append(result.Issues, Issue{Kind: "landmark_font_drift", Severity: SeverityError, Message: fmt.Sprintf("landmark %q font changed from %q to %q", landmark.Name, goldenSpan.Font, candidateSpan.Font), Target: landmark.Name})
	}
	if abs(candidateSpan.FontSize-goldenSpan.FontSize) > 0.25 {
		result.Issues = append(result.Issues, Issue{Kind: "landmark_font_size_drift", Severity: SeverityError, Message: fmt.Sprintf("landmark %q font size changed from %.2fpt to %.2fpt", landmark.Name, goldenSpan.FontSize, candidateSpan.FontSize), Target: landmark.Name})
	}
	if abs(candidateSpan.X-goldenSpan.X) > 3 {
		result.Issues = append(result.Issues, Issue{Kind: "landmark_horizontal_drift", Severity: SeverityError, Message: fmt.Sprintf("landmark %q horizontal position drifted by %.2fpt", landmark.Name, candidateSpan.X-goldenSpan.X), Target: landmark.Name})
	}
}

func findSpan(spans []TextSpan, text string) (TextSpan, bool) {
	needle := normalizeText(text)
	for _, span := range spans {
		value := normalizeText(span.Text)
		if value != "" && (strings.Contains(value, needle) || strings.Contains(needle, value)) {
			return span, true
		}
	}
	return TextSpan{}, false
}

func normalizeFont(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if index := strings.Index(value, "+"); index >= 0 {
		value = value[index+1:]
	}
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(value)
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func findPage(pages []string, needle string) int {
	needle = normalizeText(needle)
	if needle == "" {
		return 0
	}
	for index, page := range pages {
		if strings.Contains(normalizeText(page), needle) {
			return index + 1
		}
	}
	return 0
}

func normalizeText(value string) string {
	value = strings.ReplaceAll(value, "\u00a0", " ")
	value = strings.ReplaceAll(value, "\u3000", " ")
	return strings.Join(strings.Fields(value), "")
}

func hasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			return true
		}
	}
	return false
}
