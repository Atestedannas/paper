package fileprocessor

// FormatLockManager 只锁定“类型 + 当前段落数”均已验证通过的结果。
// 分类数量变化时锁自动失效，避免新误分类段落被错误跳过。
type FormatLockManager struct {
	verifiedCounts map[string]int
}

func NewFormatLockManager() *FormatLockManager {
	return &FormatLockManager{verifiedCounts: map[string]int{}}
}

func (m *FormatLockManager) Lock(paragraphType string, paragraphCount int) {
	if paragraphCount > 0 {
		m.verifiedCounts[paragraphType] = paragraphCount
	}
}

func (m *FormatLockManager) IsLocked(paragraphType string, paragraphCount int) bool {
	count, ok := m.verifiedCounts[paragraphType]
	return ok && count == paragraphCount
}

func (m *FormatLockManager) Unlock(paragraphType string) {
	delete(m.verifiedCounts, paragraphType)
}
