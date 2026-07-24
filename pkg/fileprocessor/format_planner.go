package fileprocessor

import "fmt"

type FormatStep string

const (
	FormatStepSection      FormatStep = "section"
	FormatStepPageSetup    FormatStep = "page_setup"
	FormatStepHeaderFooter FormatStep = "header_footer"
	FormatStepClassify     FormatStep = "classify"
	FormatStepParagraphs   FormatStep = "paragraphs"
	FormatStepPageBreaks   FormatStep = "page_breaks"
	FormatStepTables       FormatStep = "tables"
	FormatStepVerify       FormatStep = "verify"
)

type FormatPlanStep struct {
	ID        FormatStep
	DependsOn []FormatStep
}

type FormatPlanner struct {
	steps []FormatPlanStep
}

func NewFormatPlanner() *FormatPlanner {
	return &FormatPlanner{steps: []FormatPlanStep{
		{ID: FormatStepSection},
		{ID: FormatStepPageSetup, DependsOn: []FormatStep{FormatStepSection}},
		{ID: FormatStepHeaderFooter, DependsOn: []FormatStep{FormatStepPageSetup}},
		{ID: FormatStepClassify, DependsOn: []FormatStep{FormatStepSection}},
		{ID: FormatStepParagraphs, DependsOn: []FormatStep{FormatStepClassify}},
		{ID: FormatStepPageBreaks, DependsOn: []FormatStep{FormatStepParagraphs}},
		{ID: FormatStepTables, DependsOn: []FormatStep{FormatStepParagraphs}},
		{ID: FormatStepVerify, DependsOn: []FormatStep{FormatStepHeaderFooter, FormatStepPageBreaks, FormatStepTables}},
	}}
}

func (p *FormatPlanner) Plan() ([]FormatStep, error) {
	pending := append([]FormatPlanStep(nil), p.steps...)
	done := map[FormatStep]bool{}
	plan := make([]FormatStep, 0, len(pending))
	for len(pending) > 0 {
		progress := false
		next := pending[:0]
		for _, step := range pending {
			ready := true
			for _, dependency := range step.DependsOn {
				if !done[dependency] {
					ready = false
					break
				}
			}
			if !ready {
				next = append(next, step)
				continue
			}
			done[step.ID] = true
			plan = append(plan, step.ID)
			progress = true
		}
		if !progress {
			return nil, fmt.Errorf("format plan contains a dependency cycle")
		}
		pending = next
	}
	return plan, nil
}
