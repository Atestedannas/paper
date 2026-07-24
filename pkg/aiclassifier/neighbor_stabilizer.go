package aiclassifier

import "math"

func stabilizeClassificationByNeighbors(features []ParagraphFeature, results []ClassifyResult) []ClassifyResult {
	if len(features) != len(results) || len(results) < 3 {
		return results
	}
	stabilized := append([]ClassifyResult(nil), results...)
	for index := 1; index+1 < len(results); index++ {
		previous, current, next := results[index-1], results[index], results[index+1]
		if previous.Label == "" || previous.Label != next.Label || current.Label == previous.Label {
			continue
		}
		if current.Confidence >= 0.90 || !similarParagraphFormat(features[index], features[index-1]) ||
			!similarParagraphFormat(features[index], features[index+1]) {
			continue
		}
		stabilized[index] = ClassifyResult{
			Label:      previous.Label,
			Confidence: math.Min(previous.Confidence, next.Confidence),
			Source:     "neighbor_vote",
			Level:      detectLevelFromLabel(previous.Label),
		}
	}
	return stabilized
}

func similarParagraphFormat(left, right ParagraphFeature) bool {
	return math.Abs(left.FontSizePt-right.FontSizePt) <= 0.5 &&
		left.IsBold == right.IsBold &&
		left.Alignment == right.Alignment
}
