package discovery

import (
	"fmt"
	"net"
	"testing"
)

func addresses(cidrs ...string) []net.Addr {
	var out []net.Addr
	for _, cidr := range cidrs {
		ip, n, _ := net.ParseCIDR(cidr)
		n.IP = ip
		out = append(out, n)
	}
	return out
}

func TestSelectSubnet(t *testing.T) {
	for _, test := range []struct{ cidr, broadcast string }{
		{"192.168.9.130/25", "192.168.9.255"}, {"10.20.1.4/16", "10.20.255.255"}, {"172.16.2.5/30", "172.16.2.7"},
	} {
		s, err := selectSubnet(net.FlagUp, addresses(test.cidr, "fe80::1/64"))
		if err != nil || s.broadcast.String() != test.broadcast {
			t.Fatalf("%s: %v %v", test.cidr, s, err)
		}
	}
	for _, cidrs := range [][]string{{"8.8.8.8/24"}, {"10.0.0.1/8"}, {"10.0.0.1/31"}, {"192.168.1.0/24"}, {"192.168.1.255/24"}, {"192.168.1.1/24", "10.0.0.1/24"}} {
		if _, err := selectSubnet(net.FlagUp, addresses(cidrs...)); err == nil {
			t.Fatalf("accepted %v", cidrs)
		}
	}
	for _, flags := range []net.Flags{0, net.FlagUp | net.FlagLoopback} {
		if _, err := selectSubnet(flags, addresses("10.0.0.1/24")); err == nil {
			t.Fatal("accepted interface flags")
		}
	}
}

func fixture(uid string) []byte {
	p := make([]byte, 276)
	copy(p, []byte{0x22, 0x11, 1, 8})
	copy(p[84:90], []byte{2, 1, 2, 3, 4, 5})
	copy(p[124:156], uid)
	return p
}

func TestResponseBoundariesAndDedup(t *testing.T) {
	s, _ := selectSubnet(net.FlagUp, addresses("192.168.1.5/24"))
	var r results
	for _, sender := range []*net.UDPAddr{nil, {IP: net.ParseIP("192.168.1.6"), Port: 1}, {IP: net.ParseIP("192.168.2.6"), Port: 8600}, {IP: s.ip, Port: 8600}, {IP: s.broadcast, Port: 8600}, {IP: s.network.IP, Port: 8600}} {
		if err := r.accept(fixture("UNIT"), sender, s); err != nil {
			t.Fatal(err)
		}
	}
	valid := &net.UDPAddr{IP: net.ParseIP("192.168.1.6"), Port: 8600}
	for _, p := range [][]byte{nil, make([]byte, 65508), fixture("UNIT"), fixture("UNIT")} {
		if err := r.accept(p, valid, s); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.devices) != 1 {
		t.Fatal("invalid responses accepted or duplicate retained")
	}
	for i := 1; i < 64; i++ {
		if err := r.accept(fixture(fmt.Sprintf("UNIT-%03d", i)), valid, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.accept(fixture("EXCESS"), valid, s); err == nil {
		t.Fatal("device limit absent")
	}
	if len(r.devices) != 64 {
		t.Fatal("excess stored")
	}
	r = results{}
	for i := 0; i < 256; i++ {
		if err := r.accept(nil, nil, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.accept(nil, nil, s); err == nil {
		t.Fatal("packet limit absent")
	}
}

func TestSortedResults(t *testing.T) {
	r := results{devices: []Device{{UID: "B"}, {UID: "A", MAC: "b"}, {UID: "A", MAC: "a", IP: "2"}, {UID: "A", MAC: "a", IP: "1"}}}
	got := r.sorted()
	if got[0].IP != "1" || got[1].IP != "2" || got[2].MAC != "b" || got[3].UID != "B" {
		t.Fatal(got)
	}
	if (&results{}).sorted() == nil {
		t.Fatal("nil empty result")
	}
}
