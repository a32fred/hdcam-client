//go:build linux

package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"time"
)

func Run(ctx context.Context, interfaceName string, duration time.Duration) ([]Device, error) {
	if duration < time.Second || duration > 10*time.Second {
		return nil, errors.New("discovery duration must be 1 through 10 seconds")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("select discovery interface: %w", err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("read interface addresses: %w", err)
	}
	selected, err := selectSubnet(iface.Flags, addresses)
	if err != nil {
		return nil, err
	}
	lc := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		err := raw.Control(func(fd uintptr) {
			socketErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			if socketErr == nil {
				socketErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface.Name)
			}
		})
		if err != nil {
			return err
		}
		return socketErr
	}}
	var packetConn net.PacketConn
	for port := 6801; port <= 6803; port++ {
		packetConn, err = lc.ListenPacket(ctx, "udp4", net.JoinHostPort(selected.ip.String(), strconv.Itoa(port)))
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, fmt.Errorf("bind discovery socket: %w", err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("discovery ports unavailable: %w", err)
	}
	defer packetConn.Close()
	stop := context.AfterFunc(ctx, func() { packetConn.Close() })
	defer stop()
	conn := packetConn.(*net.UDPConn)
	end := time.Now().Add(duration)
	nextSend := time.Now()
	attempts := 0
	var found results
	buffer := make([]byte, 65508)
	for {
		if err := ctx.Err(); err != nil {
			return found.sorted(), err
		}
		now := time.Now()
		if !now.Before(end) {
			return found.sorted(), nil
		}
		if attempts < 3 && !now.Before(nextSend) {
			if err := conn.SetWriteDeadline(end); err != nil {
				return found.sorted(), err
			}
			if _, err := conn.WriteToUDP([]byte{0x22, 0x11, 0x01, 0x01}, &net.UDPAddr{IP: selected.broadcast, Port: 8600}); err != nil {
				if ctx.Err() != nil {
					return found.sorted(), ctx.Err()
				}
				return found.sorted(), fmt.Errorf("send discovery probe: %w", err)
			}
			attempts++
			nextSend = time.Now().Add(100 * time.Millisecond)
		}
		deadline := end
		if attempts < 3 && nextSend.Before(deadline) {
			deadline = nextSend
		}
		if err := conn.SetReadDeadline(deadline); err != nil {
			return found.sorted(), err
		}
		n, sender, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return found.sorted(), ctx.Err()
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return found.sorted(), fmt.Errorf("receive discovery response: %w", err)
		}
		if err := found.accept(buffer[:n], sender, selected); err != nil {
			return found.sorted(), err
		}
	}
}
