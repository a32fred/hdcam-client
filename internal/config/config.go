package config

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Config struct {
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
}

func Load(path string) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return c, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return c, errors.New("config must be a private regular file (permissions 0600, maximum 64 KiB)")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return c, err
	}
	if len(data) > 65536 || !utf8.Valid(data) {
		return c, errors.New("invalid config size or encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return c, errors.New("invalid configuration JSON or unknown field")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return c, errors.New("config must contain one JSON object")
	}
	if err := c.ValidateAccess(); err != nil {
		return c, err
	}
	prefix, err := netip.ParsePrefix(c.AllowedSubnet)
	if err != nil || !prefix.Addr().Is4() {
		return c, errors.New("allowed_subnet must be an IPv4 private subnet")
	}
	allowed := false
	for _, private := range []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16")} {
		if private.Contains(prefix.Addr()) && prefix.Bits() >= private.Bits() {
			allowed = true
		}
	}
	if !allowed {
		return c, errors.New("allowed_subnet must stay inside an IPv4 private range")
	}
	c.AllowedSubnet = prefix.Masked().String()
	base, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return c, err
	}
	resolve := func(p string) string {
		if !filepath.IsAbs(p) {
			return filepath.Join(base, p)
		}
		return filepath.Clean(p)
	}
	for _, field := range []struct {
		name       string
		value      *string
		machine    uint16
		executable bool
	}{
		{"qemu", &c.QEMU, 62, true}, {"bridge", &c.Bridge, 183, true}, {"library", &c.Library, 183, false}, {"network_guard", &c.NetworkGuard, 183, false},
	} {
		if *field.value == "" || strings.ContainsAny(*field.value, "\x00\n\r:") {
			return c, fmt.Errorf("invalid %s path", field.name)
		}
		*field.value = resolve(*field.value)
		if !validELF(*field.value, field.machine, field.executable) {
			return c, fmt.Errorf("%s must be a compatible ELF file", field.name)
		}
	}
	if c.Sysroot == "" {
		return c, errors.New("missing sysroot")
	}
	c.Sysroot = resolve(c.Sysroot)
	if !validELF(filepath.Join(c.Sysroot, "system/bin/linker64"), 183, true) {
		return c, errors.New("sysroot is missing an ARM64 Android linker")
	}
	if len(c.LibraryPath) == 0 {
		return c, errors.New("library_path must contain runtime directories")
	}
	for i, p := range c.LibraryPath {
		if p == "" || strings.ContainsAny(p, "\x00\n\r:") {
			return c, errors.New("invalid library_path directory")
		}
		p = resolve(p)
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			return c, errors.New("library_path directory is unavailable")
		}
		c.LibraryPath[i] = p
	}
	return c, nil
}

func (c Config) ValidateAccess() error {
	for _, field := range []struct {
		name, value string
		limit       int
	}{
		{"uid", c.UID, 128}, {"username", c.Username, 32}, {"password", c.Password, 64}, {"sdk_init", c.SDKInit, 4096},
	} {
		if len(field.value) == 0 || len(field.value) > field.limit || strings.ContainsRune(field.value, 0) {
			return fmt.Errorf("invalid %s", field.name)
		}
	}
	return nil
}

func validELF(path string, machine uint16, executable bool) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || (executable && info.Mode().Perm()&0111 == 0) {
		return false
	}
	var h [20]byte
	if _, err := io.ReadFull(f, h[:]); err != nil {
		return false
	}
	return bytes.Equal(h[:4], []byte{127, 'E', 'L', 'F'}) && h[4] == 2 && h[5] == 1 && binary.LittleEndian.Uint16(h[18:]) == machine
}
