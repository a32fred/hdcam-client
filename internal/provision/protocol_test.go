package provision

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildRequest(t *testing.T) {
	packet, err := BuildRequest("TEST-UNIT", " Lab Network ", " test key ")
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, 174)
	copy(want, []byte{0x23, 0x11, 0x04, 0x08})
	copy(want[4:36], "TEST-UNIT")
	copy(want[36:100], " Lab Network ")
	copy(want[100:164], " test key ")
	if !bytes.Equal(packet, want) {
		t.Fatal("unexpected request layout")
	}
	for _, password := range []string{"", strings.Repeat("x", 8), strings.Repeat("x", 63)} {
		if _, err := BuildRequest(strings.Repeat("U", 31), strings.Repeat("S", 32), password); err != nil {
			t.Fatal("valid boundary rejected")
		}
	}
}

func TestBuildRejectsInvalidFields(t *testing.T) {
	cases := [][3]string{
		{"", "Lab", ""}, {strings.Repeat("U", 32), "Lab", ""},
		{"UNIT", "", ""}, {"UNIT", strings.Repeat("S", 33), ""},
		{"UNIT", "Lab", "short"}, {"UNIT", "Lab", strings.Repeat("x", 64)},
	}
	for _, bad := range []string{"bad\x00value", "bad\nvalue", "bad\x7fvalue", "café", "unit😀"} {
		cases = append(cases, [3]string{bad, "Lab", ""}, [3]string{"UNIT", bad, ""}, [3]string{"UNIT", "Lab", bad})
	}
	for i, c := range cases {
		if packet, err := BuildRequest(c[0], c[1], c[2]); err == nil || packet != nil {
			t.Fatalf("case %d accepted invalid fields", i)
		}
	}
}

func discovery() []byte {
	packet := make([]byte, 276)
	copy(packet, []byte{0x22, 0x11, 0x01, 0x08})
	copy(packet[84:90], []byte{0x02, 0x10, 0x20, 0x30, 0x40, 0x50})
	copy(packet[124:156], "TEST-UNIT")
	return packet
}

func TestParseDiscovery(t *testing.T) {
	for _, kind := range []byte{1, 3} {
		packet := discovery()
		packet[2] = kind
		device, err := ParseDiscovery(packet)
		if err != nil || device.UID != "TEST-UNIT" || !bytes.Equal(device.MAC, packet[84:90]) {
			t.Fatal("valid discovery rejected")
		}
		packet[84] = 0x04
		if device.MAC[0] != 0x02 {
			t.Fatal("MAC aliases input packet")
		}
	}
	packet := append(discovery(), make([]byte, maxDatagram-276)...)
	copy(packet[124:156], strings.Repeat("U", 31)+"\x00")
	if _, err := ParseDiscovery(packet); err != nil {
		t.Fatal("valid discovery boundary rejected")
	}
}

func TestDiscoveryRejectsMalformedPackets(t *testing.T) {
	mutations := []func([]byte) []byte{
		func(p []byte) []byte { return p[:275] },
		func(p []byte) []byte { return append(p, make([]byte, maxDatagram+1-len(p))...) },
		func(p []byte) []byte { p[0] = 0; return p },
		func(p []byte) []byte { p[1] = 0; return p },
		func(p []byte) []byte { p[2] = 5; return p },
		func(p []byte) []byte { p[3] = 1; return p },
		func(p []byte) []byte { clear(p[84:90]); return p },
		func(p []byte) []byte { copy(p[84:90], bytes.Repeat([]byte{255}, 6)); return p },
		func(p []byte) []byte { p[84] |= 1; return p },
		func(p []byte) []byte { p[124] = 0; return p },
		func(p []byte) []byte { copy(p[124:156], strings.Repeat("U", 32)); return p },
		func(p []byte) []byte { p[125] = '\n'; return p },
		func(p []byte) []byte { copy(p[124:156], "unité"); return p },
	}
	if _, err := ParseDiscovery(nil); err == nil {
		t.Fatal("nil discovery accepted")
	}
	for i, mutate := range mutations {
		if _, err := ParseDiscovery(mutate(discovery())); err == nil {
			t.Fatalf("malformed discovery %d accepted", i)
		}
	}
}

func TestObservedDiscoveryLayout(t *testing.T) {
	packet := append(discovery(), make([]byte, 462-276)...)
	copy(packet[92:116], "OBSERVED-TEST-UNIT")
	copy(packet[124:156], "PLAUSIBLE-WRONG-UID")
	device, err := ParseDiscovery(packet)
	if err != nil || device.UID != "OBSERVED-TEST-UNIT" {
		t.Fatal("observed layout UID not selected")
	}
	copy(packet[92:116], strings.Repeat("U", 23)+"\x00")
	if device, err := ParseDiscovery(packet); err != nil || device.UID != strings.Repeat("U", 23) {
		t.Fatal("observed layout boundary rejected")
	}
	copy(packet[92:116], strings.Repeat("U", 24))
	if _, err := ParseDiscovery(packet); err == nil {
		t.Fatal("unterminated observed UID accepted")
	}
	packet[92] = 0
	if _, err := ParseDiscovery(packet); err == nil {
		t.Fatal("empty observed UID accepted")
	}
}

func ack() []byte {
	packet, _ := BuildRequest("TEST-UNIT", " Lab ", "")
	packet[2] = 5
	return packet
}

func TestParseAck(t *testing.T) {
	packet := ack()
	packet[0], packet[1] = 0xa0, 0xb0 // The echo's first two bytes are not established invariants.
	copy(packet[100:164], "ignored echo content")
	if err := ParseAck(packet, "TEST-UNIT", " Lab "); err != nil {
		t.Fatal(err)
	}
	for _, expected := range [][2]string{{"OTHER", " Lab "}, {"TEST-UNIT", "Lab"}, {"", " Lab "}, {"TEST-UNIT", "café"}} {
		if err := ParseAck(packet, expected[0], expected[1]); err == nil {
			t.Fatal("invalid expected identity accepted")
		}
	}
	packet, _ = BuildRequest(strings.Repeat("U", 31), strings.Repeat("S", 32), "")
	packet[2] = 5
	packet = append(packet, make([]byte, maxDatagram-len(packet))...)
	if err := ParseAck(packet, strings.Repeat("U", 31), strings.Repeat("S", 32)); err != nil {
		t.Fatal("valid echo boundary rejected")
	}
}

func TestAckRejectsMalformedPackets(t *testing.T) {
	mutations := []func([]byte) []byte{
		func(p []byte) []byte { return p[:173] },
		func(p []byte) []byte { return append(p, make([]byte, maxDatagram+1-len(p))...) },
		func(p []byte) []byte { p[2] = 4; return p },
		func(p []byte) []byte { p[3] = 0; return p },
		func(p []byte) []byte { copy(p[4:36], strings.Repeat("U", 32)); return p },
		func(p []byte) []byte { copy(p[36:100], strings.Repeat("S", 64)); return p },
		func(p []byte) []byte { p[4] = 0; return p },
		func(p []byte) []byte { p[36] = 0; return p },
		func(p []byte) []byte { p[5] = '\t'; return p },
		func(p []byte) []byte { copy(p[36:100], "réseau"); return p },
	}
	if err := ParseAck(nil, "TEST-UNIT", " Lab "); err == nil {
		t.Fatal("nil echo accepted")
	}
	for i, mutate := range mutations {
		if err := ParseAck(mutate(ack()), "TEST-UNIT", " Lab "); err == nil {
			t.Fatalf("malformed echo %d accepted", i)
		}
	}
}
