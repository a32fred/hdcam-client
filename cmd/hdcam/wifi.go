package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"example.com/hdcam-client/internal/config"
	"example.com/hdcam-client/internal/provision"
	"flag"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

type wifiProfile struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

func loadWiFi(path string) (wifiProfile, error) {
	var profile wifiProfile
	file, err := os.Open(path)
	if err != nil {
		return profile, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return profile, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return profile, errors.New("Wi-Fi profile must be a private regular file (0600)")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return profile, err
	}
	if len(data) > 4096 || !utf8.Valid(data) {
		return profile, errors.New("invalid Wi-Fi profile size or encoding")
	}
	// Decode errors can contain input fragments; expose a fixed message instead.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&profile) != nil {
		return wifiProfile{}, errors.New("invalid Wi-Fi profile JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return wifiProfile{}, errors.New("unexpected trailing Wi-Fi profile data")
	}
	return profile, nil
}
func wifiPlanCommand(args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("wifi-plan", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	camera := flags.String("config", "", "private camera profile with known UID")
	network := flags.String("wifi-profile", "", "private target 2.4GHz Wi-Fi profile")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *camera == "" || *network == "" {
		return 2
	}
	c, err := config.Load(*camera)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	wifi, err := loadWiFi(*network)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	request, err := provision.BuildRequest(c.UID, wifi.SSID, wifi.Password)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	plan := struct {
		PacketBytes       int  `json:"packet_bytes"`
		DestinationPort   int  `json:"destination_port"`
		Plaintext         bool `json:"credentials_in_plaintext"`
		RequiresCameraAP  bool `json:"requires_camera_configuration_ap"`
		SendingEnabled    bool `json:"sending_enabled"`
		HardwareValidated bool `json:"hardware_validated"`
	}{len(request), 8600, true, true, false, false}
	if err := json.NewEncoder(out).Encode(plan); err != nil {
		return 1
	}
	return 0
}
