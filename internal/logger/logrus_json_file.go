package logger

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

var (
	logrusFileMu sync.Mutex
	logrusFile   *os.File
)

// InitLogrusJSONFile 将全局 logrus 设为 JSON 格式并写入文件（追加）。
// path 为空时读取环境变量 LOGRUS_JSON_FILE；仍为空则不做任何事。
//
// 若 LOGRUS_JSON_ALSO_STDERR=1/true/yes，则同时写入文件与 os.Stderr（JSON 双份）。
// 可重复调用：若已成功打开过同一配置则跳过；路径变化会先关旧文件再打开新路径。
func InitLogrusJSONFile(path string) error {
	if strings.TrimSpace(path) == "" {
		path = strings.TrimSpace(os.Getenv("LOGRUS_JSON_FILE"))
	}
	if path == "" {
		return nil
	}

	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	logrusFileMu.Lock()
	defer logrusFileMu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if logrusFile != nil {
		_ = logrusFile.Close()
	}
	logrusFile = f

	logrus.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
	})
	logrus.SetLevel(logrus.InfoLevel)

	alsoStderr := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOGRUS_JSON_ALSO_STDERR"))) {
	case "1", "true", "yes", "on":
		alsoStderr = true
	}
	if alsoStderr {
		logrus.SetOutput(io.MultiWriter(f, os.Stderr))
	} else {
		logrus.SetOutput(f)
	}

	logrus.WithField("path", path).Info("logrus JSON file logging enabled")
	return nil
}

// CloseLogrusFile 关闭 JSON 日志文件（一般在进程退出前调用；可选）。
func CloseLogrusFile() {
	logrusFileMu.Lock()
	defer logrusFileMu.Unlock()
	if logrusFile != nil {
		_ = logrusFile.Close()
		logrusFile = nil
	}
}
