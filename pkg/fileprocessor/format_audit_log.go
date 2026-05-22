package fileprocessor

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	auditLogMu        sync.Mutex
	auditFileLogger   *log.Logger
	auditFileDisabled bool
)

// formatAuditJSONLine 生成与控制台一致的审计 JSON 一行（含路径缩短）。
func formatAuditJSONLine(phase, action string, fields map[string]interface{}) (string, error) {
	full := strings.TrimSpace(os.Getenv("PAPER_FORMAT_AUDIT_FULL_PATHS"))
	fullPaths := full == "1" || strings.EqualFold(full, "true") || strings.EqualFold(full, "yes")

	payload := map[string]interface{}{"phase": phase, "action": action}
	for k, v := range fields {
		if !fullPaths {
			if s, ok := v.(string); ok && s != "" && (strings.Contains(s, string(filepath.Separator)) || strings.Contains(s, "/")) {
				if strings.HasSuffix(strings.ToLower(k), "_path") || k == "doc_path" || k == "template_path" || k == "output_path" ||
					k == "input_path" || k == "corrected" || k == "template" {
					v = filepath.Base(s)
				}
			}
		}
		payload[k] = v
	}
	if !fullPaths {
		payload["paths_short"] = true
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// writeFormatAuditLogFile 将审计 JSON 追加到日志文件（使用标准库 log，带时间戳）。
// PAPER_FORMAT_AUDIT_FILE：文件路径；未设置则写入 logs/paper_format_audit.log（相对进程工作目录，一般为 backend）。
// 设为 0 / off / false 则关闭写文件。
func writeFormatAuditLogFile(jsonLine string) {
	v := strings.TrimSpace(os.Getenv("PAPER_FORMAT_AUDIT_FILE"))
	if v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") || strings.EqualFold(v, "no") {
		return
	}

	auditLogMu.Lock()
	defer auditLogMu.Unlock()

	if auditFileDisabled {
		return
	}

	if auditFileLogger == nil {
		path := v
		if path == "" {
			path = filepath.Join("logs", "paper_format_audit.log")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			log.Printf("[PAPER_FORMAT_AUDIT] 无法创建日志目录 %q: %v", filepath.Dir(path), err)
			auditFileDisabled = true
			return
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("[PAPER_FORMAT_AUDIT] 无法打开审计日志文件 %q: %v", path, err)
			auditFileDisabled = true
			return
		}
		auditFileLogger = log.New(f, "[PAPER_FORMAT_AUDIT] ", log.LstdFlags|log.Lmicroseconds)
	}

	auditFileLogger.Println(jsonLine)
}
