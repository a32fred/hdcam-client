package config

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureConfig(t *testing.T) (string, Config) {
	t.Helper()
	dir := t.TempDir()
	elf := func(path string, machine uint16) string {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		h := make([]byte, 64)
		copy(h, "\x7fELF")
		h[4], h[5] = 2, 1
		binary.LittleEndian.PutUint16(h[18:], machine)
		if err := os.WriteFile(path, h, 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	c := Config{UID: "TEST-UNIT", Username: "example-user", Password: "private-test-value", SDKInit: "private-test-init", AllowedSubnet: "192.168.56.0/24"}
	c.QEMU = elf(filepath.Join(dir, "qemu"), 62)
	c.Bridge = elf(filepath.Join(dir, "bridge"), 183)
	c.Library = elf(filepath.Join(dir, "sdk.so"), 183)
	c.NetworkGuard = elf(filepath.Join(dir, "guard.so"), 183)
	c.Sysroot = filepath.Join(dir, "android")
	elf(filepath.Join(c.Sysroot, "system/bin/linker64"), 183)
	c.LibraryPath = []string{dir}
	path := filepath.Join(dir, "config.json")
	write := func(c Config) {
		b, _ := json.Marshal(c)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(c)
	return path, c
}

func TestLoadConfig(t *testing.T) {
	path, want := fixtureConfig(t)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.UID != want.UID || got.Library != want.Library || got.Password != want.Password {
		t.Fatal("configuration changed")
	}
}

func TestLoadRejectsUnsafeOrInvalidConfig(t *testing.T) {
	for _, change := range []func(*Config){
		func(c *Config) { c.Password = "" },
		func(c *Config) { c.SDKInit = "" },
		func(c *Config) { c.UID += "\x00" },
		func(c *Config) { c.AllowedSubnet = "0.0.0.0/0" },
		func(c *Config) { c.AllowedSubnet = "8.8.8.0/24" },
		func(c *Config) { c.Bridge = c.QEMU },
	} {
		path, c := fixtureConfig(t)
		change(&c)
		b, _ := json.Marshal(c)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("invalid config accepted")
		} else if strings.Contains(err.Error(), "private-test-value") || strings.Contains(err.Error(), "private-test-init") {
			t.Fatal("secret in error")
		}
	}
}

func TestLoadRejectsSharedPermissionsAndUnknownFields(t *testing.T) {
	path, _ := fixtureConfig(t)
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("publicly readable config accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unknown fields accepted")
	}
}
