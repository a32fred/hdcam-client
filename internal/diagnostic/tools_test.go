package diagnostic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckToolsDoesNotExecute(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	tool := filepath.Join(dir, "fixture-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	results := CheckTools([]string{"fixture-tool", "missing-tool"})
	if len(results) != 2 || results[0].Name != "fixture-tool" || !results[0].Available || results[0].Path != tool || results[1].Available || results[1].Path != "" {
		t.Fatalf("results: %+v", results)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("tool executed or unexpected stat error: %v", err)
	}
}

func TestLookupErrorDoesNotReturnPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("PATH", ".")
	if err := os.WriteFile("relative-tool", []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	results := CheckTools([]string{"relative-tool"})
	if results[0].Available || results[0].Path != "" {
		t.Fatalf("failed lookup leaked path: %+v", results[0])
	}
}
