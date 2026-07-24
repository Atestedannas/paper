package aiclassifier

import (
	"fmt"
	"log"
	"os"
	"time"
)

// ThesisZone 论文区段枚举（按出现顺序排列）
type ThesisZone int

const (
	ZoneCover            ThesisZone = iota // 0: 封面 & 原创性声明
	ZoneAbstract                           // 1: 中文摘要
	ZoneEnAbstract                         // 2: 英文摘要
	ZoneTOC                                // 3: 目录
	ZoneBody                               // 4: 正文（含各级标题）
	ZoneReferences                         // 5: 参考文献
	ZoneAcknowledgements                   // 6: 致谢
	ZoneAppendix                           // 7: 附录
)

// ThesisStateMachine 论文结构状态机
// 状态只能正向推进（0→1→2...），不允许回退
type ThesisStateMachine struct {
	zone ThesisZone
}

// NewThesisStateMachine 创建状态机，初始在封面区
func NewThesisStateMachine() *ThesisStateMachine {
	return &ThesisStateMachine{zone: ZoneCover}
}

// tryAdvance 尝试根据强信号标签推进区段（只前进，不回退）
func (sm *ThesisStateMachine) tryAdvance(label string, text string) {
	switch {
	case smContainsAppendix(text):
		sm.zone = ZoneAppendix
	case smContainsAcknowledgement(text):
		sm.zone = ZoneAcknowledgements
	case sm.zone == ZoneReferences &&
		(label == TypeHeading1 || label == TypeHeading2 || label == TypeHeading3) &&
		!smLooksTOCEntry(text):
		sm.zone = ZoneBody
	case sm.zone <= ZoneCover && (label == TypeAbstractTitle || label == TypeAbstract):
		sm.zone = ZoneAbstract
	case sm.zone <= ZoneAbstract && (label == TypeEnAbstractTitle || label == TypeEnAbstract || label == TypeEnKeywords):
		sm.zone = ZoneEnAbstract
	case sm.zone <= ZoneEnAbstract && (label == TypeTOCTitle || label == TypeTOC):
		sm.zone = ZoneTOC
	// 从TOC区推进到正文区：排除目录条目（含"．．"或"……"的行是目录条目，不能触发ZoneBody）
	case sm.zone <= ZoneTOC && (label == TypeHeading1 || label == TypeHeading2 || label == TypeHeading3 || label == TypeBody) && !smLooksTOCEntry(text):
		sm.zone = ZoneBody
	// 参考文献信号：必须已在正文区才能推进（防止TOC中"参考文献"条目误触发）
	case sm.zone == ZoneBody && label == TypeReferencesTitle && !smLooksTOCEntry(text):
		// #region agent log H5
		smDbgLog("H5", "thesis_state_machine.go:tryAdvance",
			"advancing to ZoneReferences",
			fmt.Sprintf(`{"runId":"post-fix2","text":%q,"label":%q,"isTOCEntry":%v}`,
				smTruncate(text, 30), label, smLooksTOCEntry(text)))
		// #endregion agent log H5
		sm.zone = ZoneReferences
	}
}

// Reclassify 用状态机对分类结果进行上下文修正，返回修正后的标签
func (sm *ThesisStateMachine) Reclassify(label string, text string) string {
	sm.tryAdvance(label, text)

	switch sm.zone {
	case ZoneCover:
		if label == TypeBody || label == TypeTitle {
			return TypeCover
		}
		return label

	case ZoneAbstract:
		if label == TypeBody {
			return TypeAbstract
		}
		return label

	case ZoneEnAbstract:
		if label == TypeBody || label == TypeAbstract {
			return TypeEnAbstract
		}
		return label

	case ZoneTOC:
		// TOC区内：body和标题格式的段落都是目录条目，统一归为TypeTOC
		// heading_1不转换（可能是"目 录"这个标题本身）
		if label == TypeBody || label == TypeHeading2 || label == TypeHeading3 {
			return TypeTOC
		}
		return label

	case ZoneBody:
		if label == TypeAbstract || label == TypeCover || label == TypeTOC {
			return TypeBody
		}
		return label

	case ZoneReferences:
		if label == TypeBody {
			return TypeReferences
		}
		return label

	case ZoneAcknowledgements:
		if label == TypeBody {
			return "acknowledgements"
		}
		return label
	case ZoneAppendix:
		if label == TypeBody || label == TypeReferences {
			return "appendix_content"
		}
		return label
	}
	return label
}

func smContainsAppendix(text string) bool {
	if len([]rune(text)) > 30 {
		return false
	}
	for _, keyword := range []string{"附录", "APPENDIX", "Appendix"} {
		if smContainsStr(text, keyword) {
			return true
		}
	}
	return false
}

func smContainsAcknowledgement(text string) bool {
	if len([]rune(text)) > 20 {
		return false
	}
	for _, kw := range []string{"致谢", "致　谢", "致 谢", "ACKNOWLEDGEMENT", "Acknowledgement"} {
		if smContainsStr(text, kw) {
			return true
		}
	}
	return false
}

func smContainsStr(s, sub string) bool {
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// smTruncate 截断字符串用于日志输出
func smTruncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) > n {
		return string(runes[:n]) + "..."
	}
	return s
}

// smLooksTOCEntry 判断文本是否为目录条目（包含连续省略号/点号）
func smLooksTOCEntry(text string) bool {
	dotPatterns := []string{"．．", "……", "......", "· · ·", "···"}
	for _, p := range dotPatterns {
		if smContainsStr(text, p) {
			return true
		}
	}
	return false
}

// #region agent log H5
func smDbgLog(hypothesisId, location, message, data string) {
	f, err := os.OpenFile("debug-c190b3.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().UnixMilli()
	line := fmt.Sprintf(`{"sessionId":"c190b3","hypothesisId":%q,"location":%q,"message":%q,"data":%s,"timestamp":%d}`+"\n",
		hypothesisId, location, message, data, ts)
	f.WriteString(line)
}

// #endregion agent log H5

// ensure log is used
var _ = log.Printf
