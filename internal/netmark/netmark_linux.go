//go:build linux

package netmark

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"

	"github.com/kriakiku/potato-network/internal/config"
)

// DialContext dials with SO_MARK=MarkNoRedirect so traffic skips MITM REDIRECT.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout: 30 * time.Second,
		Control: markControl,
	}
	return d.DialContext(ctx, network, address)
}

// DialTimeout is DialContext with a fixed timeout (for TCP RTT probes).
func DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	d := &net.Dialer{
		Timeout: timeout,
		Control: markControl,
	}
	return d.Dial(network, address)
}

func markControl(network, address string, c syscall.RawConn) error {
	var opErr error
	err := c.Control(func(fd uintptr) {
		opErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, config.MarkNoRedirect)
	})
	if err != nil {
		return err
	}
	// Unit tests / unprivileged hosts: SO_MARK needs CAP_NET_ADMIN. Fall back to
	// an unmarked dial so MITM unit tests still work outside a Potato netns.
	if opErr != nil && (errors.Is(opErr, syscall.EPERM) || errors.Is(opErr, syscall.EACCES)) {
		return nil
	}
	return opErr
}
