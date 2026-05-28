//go:build !linux

package server

import (
	"context"
	"net"
)

func listen(network, addr string) (net.Listener, error) {
	return new(net.ListenConfig).Listen(context.Background(), network, addr)
}
