package goldenregression

import "testing"

func TestCompareSnapshotsPassesWhenLandmarksStayAligned(t *testing.T) {
	result := CompareSnapshots(Options{
		Candidate: PageSnapshot{Pages: []string{"封面", "题目 摘要：正文", "目录"}},
		Golden:    PageSnapshot{Pages: []string{"封面", "题目 摘要：说明", "目录"}},
		Landmarks: []Landmark{{Name: "abstract", Text: "摘要："}},
		SamePageLandmark: []SamePageLandmark{
			{Name: "title_abstract", Left: "题目", Right: "摘要："},
		},
	})
	if !result.Passed {
		t.Fatalf("Passed = false, issues = %#v", result.Issues)
	}
}

func TestCompareSnapshotsFailsWhenGoldenSamePageRuleDrifts(t *testing.T) {
	result := CompareSnapshots(Options{
		Candidate: PageSnapshot{Pages: []string{"封面", "题目", "摘要：正文"}},
		Golden:    PageSnapshot{Pages: []string{"封面", "题目 摘要：说明"}},
		SamePageLandmark: []SamePageLandmark{
			{Name: "title_abstract", Left: "题目", Right: "摘要："},
		},
	})
	if result.Passed {
		t.Fatal("Passed = true, want false")
	}
	if len(result.Issues) != 1 || result.Issues[0].Kind != "candidate_same_page_drift" {
		t.Fatalf("Issues = %#v, want candidate_same_page_drift", result.Issues)
	}
}

func TestCompareSnapshotsFailsOnPageCountDrift(t *testing.T) {
	result := CompareSnapshots(Options{
		Candidate:      PageSnapshot{Pages: []string{"一", "二", "三"}},
		Golden:         PageSnapshot{Pages: []string{"一"}},
		CheckPageCount: true,
		MaxPageDelta:   0,
	})
	if result.Passed {
		t.Fatal("Passed = true, want false")
	}
	if len(result.Issues) != 1 || result.Issues[0].Kind != "page_count_drift" {
		t.Fatalf("Issues = %#v, want page_count_drift", result.Issues)
	}
}

func TestCompareSnapshotsFailsOnLandmarkFontDrift(t *testing.T) {
	result := CompareSnapshots(Options{
		Candidate:     PageSnapshot{Pages: []string{"Abstract"}, Spans: []TextSpan{{Page: 1, Text: "Abstract", Font: "Arial", FontSize: 15, X: 72}}},
		Golden:        PageSnapshot{Pages: []string{"Abstract"}, Spans: []TextSpan{{Page: 1, Text: "Abstract", Font: "Times New Roman", FontSize: 15, X: 72}}},
		Landmarks:     []Landmark{{Name: "abstract_en", Text: "Abstract"}},
		CompareStyles: true,
	})
	if result.Passed {
		t.Fatal("Passed = true, want false")
	}
	if len(result.Issues) != 1 || result.Issues[0].Kind != "landmark_font_drift" {
		t.Fatalf("Issues = %#v", result.Issues)
	}
}
