package provision

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
)

const maxDatagram = 65507

type Device struct {
	UID string
	MAC net.HardwareAddr
}

func BuildRequest(uid, ssid, password string) ([]byte, error) {
	if !validText(uid, 1, 31) || !validText(ssid, 1, 32) ||
		(password != "" && !validText(password, 8, 63)) {
		return nil, errors.New("invalid provisioning fields")
	}
	packet := make([]byte, 174)
	copy(packet, []byte{0x23, 0x11, 0x04, 0x08})
	copy(packet[4:36], uid)
	copy(packet[36:100], ssid)
	copy(packet[100:164], password)
	return packet, nil
}

func ParseDiscovery(packet []byte) (Device, error) {
	if len(packet) < 276 || len(packet) > maxDatagram || packet[0] != 0x22 || packet[1] != 0x11 {
		return Device{}, errors.New("invalid discovery packet")
	}
	kind := binary.LittleEndian.Uint16(packet[2:4])
	if kind != 0x0801 && kind != 0x0803 {
		return Device{}, errors.New("invalid discovery packet")
	}
	// The observed 462-byte firmware reply differs from the app's static layout.
	uidField, uidMax := packet[124:156], 31
	if len(packet) == 462 {
		uidField, uidMax = packet[92:116], 23
	}
	uid, ok := field(uidField, 1, uidMax)
	mac := packet[84:90]
	if !ok || mac[0]&1 != 0 || bytes.Equal(mac, make([]byte, 6)) {
		return Device{}, errors.New("invalid discovery identity")
	}
	return Device{UID: uid, MAC: append(net.HardwareAddr(nil), mac...)}, nil
}

// ParseAck checks an unauthenticated echo, not association or recovery success.
func ParseAck(packet []byte, expectedUID, expectedSSID string) error {
	if len(packet) < 174 || len(packet) > maxDatagram ||
		binary.LittleEndian.Uint16(packet[2:4]) != 0x0805 ||
		!validText(expectedUID, 1, 31) || !validText(expectedSSID, 1, 32) {
		return errors.New("invalid provisioning echo")
	}
	uid, uidOK := field(packet[4:36], 1, 31)
	ssid, ssidOK := field(packet[36:100], 1, 32)
	if !uidOK || !ssidOK || uid != expectedUID || ssid != expectedSSID {
		return errors.New("invalid provisioning echo")
	}
	return nil
}

func field(raw []byte, min, max int) (string, bool) {
	i := bytes.IndexByte(raw, 0)
	if i < 0 {
		return "", false
	}
	s := string(raw[:i])
	return s, validText(s, min, max)
}

func validText(s string, min, max int) bool {
	if len(s) < min || len(s) > max {
		return false
	}
	for i := range len(s) {
		if s[i] < 32 || s[i] > 126 {
			return false
		}
	}
	return true
}
