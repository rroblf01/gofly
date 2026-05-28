//go:build linux

package server

import (
	"context"
	"net"
	"syscall"
)

const (
	soReusePort    = 15
	tcpQuickAck    = 12
	tcpDeferAccept = 9
)

func listen(network, addr string) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
				syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpQuickAck, 1)
				syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpDeferAccept, 1)
			})
		},
	}
	return lc.Listen(context.Background(), network, addr)
}
