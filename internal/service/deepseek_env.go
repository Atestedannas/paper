package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const defaultLegacyPaperBackendEnv = `D:\workpace\博客\paper\backend\.env`

type deepSeekCredentials struct {
	Cookie  string
	Bearer  string
	Enabled bool
}

func deepSeekCredentialsFromEnvOrFile() deepSeekCredentials {
	values := map[string]string{}
	for _, key := range []string{"DEEPSEEK_COOKIE", "DEEPSEEK_BEARER", "DEEPSEEK_ENABLED"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			values[key] = value
		}
	}
	if strings.TrimSpace(values["DEEPSEEK_COOKIE"]) == "" {
		for _, envPath := range candidateDeepSeekEnvFiles() {
			fileValues, err := godotenv.Read(envPath)
			if err != nil {
				continue
			}
			for _, key := range []string{"DEEPSEEK_COOKIE", "DEEPSEEK_BEARER", "DEEPSEEK_ENABLED"} {
				if strings.TrimSpace(values[key]) == "" {
					values[key] = strings.TrimSpace(fileValues[key])
				}
			}
			if strings.TrimSpace(values["DEEPSEEK_COOKIE"]) != "" {
				break
			}
		}
	}

	enabled := strings.TrimSpace(values["DEEPSEEK_COOKIE"]) != ""
	if raw := strings.TrimSpace(values["DEEPSEEK_ENABLED"]); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			enabled = parsed
		}
	}
	return deepSeekCredentials{
		Cookie:  strings.TrimSpace(values["DEEPSEEK_COOKIE"]),
		Bearer:  strings.TrimSpace(values["DEEPSEEK_BEARER"]),
		Enabled: enabled,
	}
}

func candidateDeepSeekEnvFiles() []string {
	paths := []string{}
	if configured := strings.TrimSpace(os.Getenv("PAPER_BACKEND_ENV_FILE")); configured != "" {
		paths = append(paths, configured)
	}
	if configured := strings.TrimSpace(os.Getenv("DEEPSEEK_ENV_FILE")); configured != "" {
		paths = append(paths, configured)
	}
	paths = append(paths,
		filepath.Clean(".env"),
		defaultLegacyPaperBackendEnv,
	)
	return uniqueNonEmptyPaths(paths)
}

func uniqueNonEmptyPaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	return result
}
