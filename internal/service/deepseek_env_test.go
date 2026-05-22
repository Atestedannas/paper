package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeepSeekCredentialsFromConfiguredEnvFile(t *testing.T) {
	t.Setenv("DEEPSEEK_COOKIE", "")
	t.Setenv("DEEPSEEK_BEARER", "")
	t.Setenv("DEEPSEEK_ENABLED", "")
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("DEEPSEEK_COOKIE=ds_session_id=test-cookie\nDEEPSEEK_BEARER=test-bearer\nDEEPSEEK_ENABLED=true\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("PAPER_BACKEND_ENV_FILE", envPath)

	creds := deepSeekCredentialsFromEnvOrFile()
	if creds.Cookie != "ds_session_id=test-cookie" {
		t.Fatalf("Cookie = %q", creds.Cookie)
	}
	if creds.Bearer != "test-bearer" {
		t.Fatalf("Bearer = %q", creds.Bearer)
	}
	if !creds.Enabled {
		t.Fatal("Enabled = false, want true")
	}
}

func TestDeepSeekCredentialsEnvironmentOverridesFile(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("DEEPSEEK_COOKIE=ds_session_id=file-cookie\nDEEPSEEK_ENABLED=true\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("PAPER_BACKEND_ENV_FILE", envPath)
	t.Setenv("DEEPSEEK_COOKIE", "ds_session_id=env-cookie")
	t.Setenv("DEEPSEEK_ENABLED", "false")

	creds := deepSeekCredentialsFromEnvOrFile()
	if creds.Cookie != "ds_session_id=env-cookie" {
		t.Fatalf("Cookie = %q", creds.Cookie)
	}
	if creds.Enabled {
		t.Fatal("Enabled = true, want false from env override")
	}
}
