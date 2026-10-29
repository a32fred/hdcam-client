package main

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTokenFixture(t *testing.T, data []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadTokenAcceptsPrivateTokenFile(t *testing.T) {
	value := strings.Repeat("a", 64)
	path := writeTokenFixture(t, []byte(value+"\n"), 0600)
	got, err := loadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != value {
		t.Fatalf("loadToken() = %q, want %q", got, value)
	}
}

func TestLoadTokenRejectsInvalidFiles(t *testing.T) {
	directory := t.TempDir()
	tests := []struct {
		name string
		make func(t *testing.T) string
	}{
		{"world-readable mode", func(t *testing.T) string {
			return writeTokenFixture(t, []byte(strings.Repeat("x", 32)), 0644)
		}},
		{"directory", func(t *testing.T) string { return directory }},
		{"empty password", func(t *testing.T) string {
			return writeTokenFixture(t, nil, 0600)
		}},
		{"multiline token", func(t *testing.T) string {
			return writeTokenFixture(t, []byte(strings.Repeat("x", 32)+"\n"+strings.Repeat("y", 32)), 0600)
		}},
		{"token over 1024 bytes", func(t *testing.T) string {
			return writeTokenFixture(t, []byte(strings.Repeat("x", 1025)), 0600)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := loadToken(tt.make(t)); err == nil {
				t.Fatal("loadToken succeeded, want error")
			}
		})
	}
}

func TestTokenCommandCreatesPrivateRandomHexWithoutPrintingSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-token")
	var out, diagnostics bytes.Buffer
	if got := tokenCommand([]string{"--output", path}, &out, &diagnostics); got != 0 {
		t.Fatalf("tokenCommand exit = %d, diagnostics %q", got, diagnostics.String())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("token mode = %04o, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value := strings.TrimSpace(string(data))
	if len(value) != 64 {
		t.Fatalf("token length = %d, want 64 hex characters", len(value))
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("token is not 32-byte hex: decoded length %d, err %v", len(decoded), err)
	}
	if strings.Contains(out.String()+diagnostics.String(), value) {
		t.Fatal("token secret was printed")
	}
	if !strings.Contains(out.String(), "private token file created") {
		t.Fatalf("success output = %q", out.String())
	}
}

func TestTokenCommandRefusesOverwriteAndPreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing-token")
	const original = "do-not-replace-this-token\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	if got := tokenCommand([]string{"--output", path}, &out, &diagnostics); got == 0 {
		t.Fatal("tokenCommand succeeded when output already existed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("existing file changed to %q", data)
	}
	if strings.Contains(out.String()+diagnostics.String(), strings.TrimSpace(original)) {
		t.Fatal("existing secret was printed")
	}
}

func TestServeCommandRejectsUnsafeOrIncompleteArgumentsBeforeLoadingFiles(t *testing.T) {
	base := []string{"--config", filepath.Join(t.TempDir(), "missing-profile"), "--token-file", filepath.Join(t.TempDir(), "missing-token")}
	tests := []struct {
		name string
		args []string
	}{
		{"missing profile", []string{"--token-file", "missing-token"}},
		{"missing token", []string{"--config", "missing-profile"}},
		{"non-loopback without TLS", append(append([]string{}, base...), "--listen", "0.0.0.0:8080")},
		{"hostname listen address", append(append([]string{}, base...), "--listen", "camera.local:8080")},
		{"invalid video mode", append(append([]string{}, base...), "--video-mode", "3")},
		{"certificate without key", append(append([]string{}, base...), "--tls-cert", "missing-cert")},
		{"key without certificate", append(append([]string{}, base...), "--tls-key", "missing-key")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			if got := serveCommand(tt.args, &out, &diagnostics); got != 2 {
				t.Fatalf("serveCommand exit = %d, want usage error 2; diagnostics: %q", got, diagnostics.String())
			}
			if strings.Contains(diagnostics.String(), "no such file") {
				t.Fatalf("serveCommand attempted to load files before rejecting arguments: %q", diagnostics.String())
			}
		})
	}
}
