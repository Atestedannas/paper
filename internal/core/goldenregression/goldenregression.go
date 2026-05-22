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
	Pages []string `json:"pages"`
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
