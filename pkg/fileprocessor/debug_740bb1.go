package fileprocessor

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const debugLogPath = "/opt/paper/debug-740bb1.log"

func debugLog(location, message string, data map[string]interface{}) {
	entry := map[string]interface{}{
		"sessionId":    "740bb1",
		"location":     location,
		"message":      message,
		"data":         data,
		"timestamp":    time.Now().UnixMilli(),
		"hypothesisId": "",
	}
	if msg, ok := data["hypothesisId"]; ok {
		entry["hypothesisId"] = msg
		delete(data, "hypothesisId")
	}
	line, _ := json.Marshal(entry)
	f, err := os.OpenFile(debugLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[debug740bb1] open error: %v\n", err)
		return
	}
	defer f.Close()
	f.Write(line)
	f.Write([]byte("\n"))
}
