package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTokenAcceptsShortPasswords(t *testing.T) {
	for _, value := range []string{"4", "4826"} {
		t.Run(value, func(t *testing.T) {
			path := writeTokenFixture(t, []byte(value+"\n"), 0600)
			got, err := loadToken(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != value {
				t.Fatalf("loadToken() = %q, want %q", got, value)
			}
		})
	}
}

func TestLoadTokenRejectsEmptyAndControlCharacters(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("\n"), []byte("pass\rword"), []byte("pass\nword"), []byte("pass\x00word")} {
		path := writeTokenFixture(t, data, 0600)
		if _, err := loadToken(path); err == nil {
			t.Errorf("loadToken(%q) succeeded, want error", data)
		}
	}
}

func TestLoadTokenRequiresPrivateMode(t *testing.T) {
	path := writeTokenFixture(t, []byte("4826"), 0644)
	if _, err := loadToken(path); err == nil {
		t.Fatal("loadToken accepted a world-readable password file")
	}
}

func TestServeCommandRejectsInvalidUsernamesBeforeLoadingFiles(t *testing.T) {
	tooLong := strings.Repeat("u", 129)
	base := []string{"--config", filepath.Join(t.TempDir(), "missing-profile"), "--token-file", filepath.Join(t.TempDir(), "missing-token")}
	for _, username := range []string{"", "bad:name", "bad\nname", "bad\x00name", tooLong} {
		t.Run(username, func(t *testing.T) {
			args := append([]string{}, base...)
			args = append(args, "--username="+username)
			var out, diagnostics bytes.Buffer
			if code := serveCommand(args, &out, &diagnostics); code != 2 {
				t.Fatalf("serveCommand exit = %d, want usage error 2; diagnostics: %q", code, diagnostics.String())
			}
			if strings.Contains(diagnostics.String(), "no such file") {
				t.Fatalf("serveCommand checked files before rejecting username: %q", diagnostics.String())
			}
		})
	}
}

func TestLoadTokenRejectsDirectory(t *testing.T) {
	if _, err := loadToken(t.TempDir()); err == nil {
		t.Fatal("loadToken accepted a directory")
	}
}

func TestLoadTokenRejectsOver1024Bytes(t *testing.T) {
	path := writeTokenFixture(t, []byte(strings.Repeat("x", 1025)), 0600)
	if _, err := loadToken(path); err == nil {
		t.Fatal("loadToken accepted password longer than 1024 bytes")
	}
}
