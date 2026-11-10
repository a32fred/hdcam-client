package discovery

import (
	"errors"
	"net"
	"sort"

	"github.com/a32fred/hdcam-client/internal/provision"
)

type Device struct {
	UID string `json:"uid"`
	MAC string `json:"mac"`
	IP  string `json:"ip"`
}

type subnet struct {
	ip        net.IP
	network   *net.IPNet
	broadcast net.IP
}

func selectSubnet(flags net.Flags, addresses []net.Addr) (subnet, error) {
	if flags&net.FlagUp == 0 || flags&net.FlagLoopback != 0 {
		return subnet{}, errors.New("discovery requires an up, non-loopback interface")
	}
	var selected subnet
	count := 0
	for _, address := range addresses {
		ip, network, err := net.ParseCIDR(address.String())
		if err != nil || ip.To4() == nil || !ip.IsPrivate() {
			continue
		}
		ones, bits := network.Mask.Size()
		if bits != 32 || ones < 16 || ones > 30 {
			continue
		}
		ip = ip.To4()
		broadcast := make(net.IP, 4)
		for i := range broadcast {
			broadcast[i] = network.IP.To4()[i] | ^network.Mask[i]
		}
		if ip.Equal(network.IP) || ip.Equal(broadcast) {
			continue
		}
		selected = subnet{ip, network, broadcast}
		count++
	}
	if count != 1 {
		return subnet{}, errors.New("interface must have exactly one eligible RFC1918 IPv4 address with prefix /16 through /30")
	}
	return selected, nil
}

type results struct {
	devices []Device
	seen    map[Device]bool
	packets int
}

func (r *results) accept(packet []byte, sender *net.UDPAddr, selected subnet) error {
	r.packets++
	if r.packets > 256 {
		return errors.New("discovery packet limit exceeded")
	}
	if sender == nil || sender.Port != 8600 || sender.IP.To4() == nil || !selected.network.Contains(sender.IP) || sender.IP.Equal(selected.ip) || sender.IP.Equal(selected.broadcast) || sender.IP.Equal(selected.network.IP) {
		return nil
	}
	parsed, err := provision.ParseDiscovery(packet)
	if err != nil {
		return nil
	}
	device := Device{parsed.UID, parsed.MAC.String(), sender.IP.String()}
	if r.seen[device] {
		return nil
	}
	if len(r.devices) >= 64 {
		return errors.New("discovery device limit exceeded")
	}
	if r.seen == nil {
		r.seen = make(map[Device]bool)
	}
	r.seen[device] = true
	r.devices = append(r.devices, device)
	return nil
}

func (r *results) sorted() []Device {
	if r.devices == nil {
		r.devices = []Device{}
	}
	sort.Slice(r.devices, func(i, j int) bool {
		a, b := r.devices[i], r.devices[j]
		if a.UID != b.UID {
			return a.UID < b.UID
		}
		if a.MAC != b.MAC {
			return a.MAC < b.MAC
		}
		return a.IP < b.IP
	})
	return r.devices
}
