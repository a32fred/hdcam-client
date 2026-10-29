package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWiFiPrivateProfilePreservesSpaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wifi.json")
	const contents = `{"ssid":" Lab Network ","password":" key with spaces "}`
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	profile, err := loadWiFi(path)
	if err != nil {
		t.Fatal(err)
	}
	if profile.SSID != " Lab Network " || profile.Password != " key with spaces " {
		t.Fatalf("profile whitespace changed: %+v", profile)
	}
}

func TestLoadWiFiRejectsUnsafeProfilesWithoutLeakingInput(t *testing.T) {
	const secret = "InjectedPrivateValue"
	tooLarge := []byte(`{"ssid":"` + strings.Repeat("x", 4097) + `"}`)
	badUTF8 := append([]byte(`{"ssid":"`), 0xff)
	badUTF8 = append(badUTF8, []byte(`"}`)...)
	for _, tt := range []struct {
		name string
		data []byte
		mode os.FileMode
		dir  bool
	}{
		{"group readable", []byte(`{"ssid":"x","password":"` + secret + `"}`), 0644, false},
		{"oversized", tooLarge, 0600, false},
		{"invalid UTF-8", badUTF8, 0600, false},
		{"unknown field", []byte(`{"ssid":"x","password":"` + secret + `","unknown":true}`), 0600, false},
		{"trailing JSON", []byte(`{"ssid":"x","password":""} {"secret":"` + secret + `"}`), 0600, false},
		{"malformed JSON", []byte(`{"ssid":"` + secret), 0600, false},
		{"directory", nil, 0700, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile")
			if tt.dir {
				if err := os.Mkdir(path, tt.mode); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, tt.data, tt.mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, tt.mode); err != nil {
					t.Fatal(err)
				}
			}
			_, err := loadWiFi(path)
			if err == nil {
				t.Fatal("unsafe profile accepted")
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("profile input leaked in error: %v", err)
			}
		})
	}
}

func TestWiFiPlanMissingFlagsAndProfileDoNotProduceAPlan(t *testing.T) {
	for _, args := range [][]string{{}, {"--config", "unused"}, {"--wifi-profile", "unused"}} {
		var out, diagnostics bytes.Buffer
		if code := wifiPlanCommand(args, &out, &diagnostics); code != 2 {
			t.Fatalf("args %v returned %d, want 2", args, code)
		}
		if out.Len() != 0 {
			t.Fatalf("args %v produced output: %s", args, out.String())
		}
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "camera.json")
	writeWiFiPlanConfigFixture(t, dir, configPath)
	var out, diagnostics bytes.Buffer
	code := wifiPlanCommand([]string{"--config", configPath, "--wifi-profile", filepath.Join(dir, "missing.json")}, &out, &diagnostics)
	if code != 1 {
		t.Fatalf("missing profile returned %d, want 1 (diagnostics: %s)", code, diagnostics.String())
	}
	if out.Len() != 0 {
		t.Fatalf("missing profile produced a plan: %s", out.String())
	}
}

func writeWiFiPlanConfigFixture(t *testing.T, dir, path string) {
	t.Helper()
	writeELF := func(path string, machine uint16, executable bool) {
		t.Helper()
		data := make([]byte, 20)
		copy(data, []byte{127, 'E', 'L', 'F', 2, 1})
		binary.LittleEndian.PutUint16(data[18:], machine)
		mode := os.FileMode(0600)
		if executable {
			mode = 0700
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	writeELF(filepath.Join(dir, "qemu"), 62, true)
	writeELF(filepath.Join(dir, "bridge"), 183, true)
	writeELF(filepath.Join(dir, "sdk.so"), 183, false)
	writeELF(filepath.Join(dir, "guard.so"), 183, false)
	sysroot := filepath.Join(dir, "sysroot")
	linker := filepath.Join(sysroot, "system", "bin", "linker64")
	if err := os.MkdirAll(filepath.Dir(linker), 0700); err != nil {
		t.Fatal(err)
	}
	writeELF(linker, 183, true)
	libdir := filepath.Join(dir, "lib")
	if err := os.Mkdir(libdir, 0700); err != nil {
		t.Fatal(err)
	}
	config := struct {
		UID           string   `json:"uid"`
		Username      string   `json:"username"`
		Password      string   `json:"password"`
		SDKInit       string   `json:"sdk_init"`
		QEMU          string   `json:"qemu"`
		Bridge        string   `json:"bridge"`
		Library       string   `json:"library"`
		NetworkGuard  string   `json:"network_guard"`
		Sysroot       string   `json:"sysroot"`
		LibraryPath   []string `json:"library_path"`
		AllowedSubnet string   `json:"allowed_subnet"`
	}{"TEST-UNIT", "user", "password", "init", "qemu", "bridge", "sdk.so", "guard.so", "sysroot", []string{"lib"}, "192.168.56.0/24"}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
