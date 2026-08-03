package fileprocessor

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

var (
	diagFile           *os.File
	diagPreviousWriter io.Writer
	diagMu             sync.Mutex
)

// InitDiagLog 初始化诊断日志文件写入器。
// 调用后会关闭之前的文件（如果存在），并创建新的日志文件。
// 同时将 Go 标准 log 包的输出也重定向到该文件，确保所有 log.Printf 输出可见。
func InitDiagLog(filePath string) error {
	diagMu.Lock()
	defer diagMu.Unlock()

	if diagFile != nil {
		diagFile.Close()
		diagFile = nil
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("diag: 无法创建日志文件 %s: %w", filePath, err)
	}

	diagFile = f
	diagPreviousWriter = log.Writer()
	log.SetOutput(f)
	fmt.Fprintf(diagFile, "=== DIAG 诊断日志开始 %s ===\n", time.Now().Format(time.RFC3339))
	return nil
}

// DiagPrintf 将格式化字符串写入诊断日志文件。
// 用法与 fmt.Printf 相同，自动追加换行并加时间戳前缀。
func DiagPrintf(format string, args ...interface{}) {
	diagMu.Lock()
	defer diagMu.Unlock()
	if diagFile != nil {
		now := time.Now().Format("15:04:05.000")
		fmt.Fprintf(diagFile, "[%s] ", now)
		fmt.Fprintf(diagFile, format+"\n", args...)
	}
}

// CloseDiagLog 关闭诊断日志文件。
func CloseDiagLog() {
	diagMu.Lock()
	defer diagMu.Unlock()
	if diagFile != nil {
		fmt.Fprintf(diagFile, "=== DIAG 诊断日志结束 %s ===\n", time.Now().Format(time.RFC3339))
		if diagPreviousWriter != nil {
			log.SetOutput(diagPreviousWriter)
			diagPreviousWriter = nil
		}
		diagFile.Close()
		diagFile = nil
	}
}
