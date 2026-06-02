//go:build linux

package server

import (
	"context"
	"net"
	"syscall"
)

func listen(network, addr string) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
				syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 15, 1) // SO_REUSEPORT
				syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
				syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, 12, 1) // TCP_QUICKACK
				syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, 9, 1)  // TCP_DEFER_ACCEPT
			})
		},
	}
	return lc.Listen(context.Background(), network, addr)
}
