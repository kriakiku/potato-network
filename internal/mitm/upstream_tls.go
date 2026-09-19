package mitm

import (
	"fmt"
	"net"

	utls "github.com/refraction-networking/utls"
)

// dialUpstreamTLS wraps raw TCP with a ClientHello that looks like Chrome.
// Plain crypto/tls is often rejected by Cloudflare (JA3) with
// "remote error: tls: handshake failure". We force ALPN to http/1.1 so the
// MITM can keep using net/http (not h2).
func dialUpstreamTLS(raw net.Conn, serverName string, insecure bool) (*utls.UConn, error) {
	cfg := &utls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: insecure,
		MinVersion:         utls.VersionTLS12,
	}
	uconn := utls.UClient(raw, cfg, utls.HelloCustom)
	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
	if err != nil {
		return nil, fmt.Errorf("chrome ClientHello spec: %w", err)
	}
	for i, ext := range spec.Extensions {
		if _, ok := ext.(*utls.ALPNExtension); ok {
			spec.Extensions[i] = &utls.ALPNExtension{
				AlpnProtocols: []string{"http/1.1"},
			}
		}
	}
	if err := uconn.ApplyPreset(&spec); err != nil {
		return nil, fmt.Errorf("apply ClientHello preset: %w", err)
	}
	if err := uconn.Handshake(); err != nil {
		return nil, err
	}
	return uconn, nil
}
