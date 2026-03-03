package formatchecker

// UniOfficeService 基于UniOffice的格式服务
type UniOfficeService struct {
	processor *UniOfficeProcessor
	port      string
}

// NewUniOfficeService 创建UniOffice服务
func NewUniOfficeService(port string) *UniOfficeService {
	if port == "" {
		port = "8003" // 默认端口
	}

	return &UniOfficeService{
		port: port,
	}
}

// Start 启动服务
func (s *UniOfficeService) Start() error {
	// 简化实现：直接返回成功
	return nil
}

// Stop 停止服务
func (s *UniOfficeService) Stop() error {
	// 简化实现：直接返回成功
	return nil
}

// ProcessFile 处理单个文件（兼容现有接口）
func (s *UniOfficeService) ProcessFile(filePath string, rules map[string]interface{}) (string, error) {
	// 简化实现：直接返回原文件路径
	return filePath, nil
}
