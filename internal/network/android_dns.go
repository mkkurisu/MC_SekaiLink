package network

import (
	"context"
	"net"
	"os"
	"strings"
	"time"
)

// InitDNSResolver configures a fallback pure-Go DNS resolver when /etc/resolv.conf
// is missing (as on Android) or when SEKAILINK_DNS_SERVERS is explicitly provided.
func InitDNSResolver() {
	serversEnv := strings.TrimSpace(os.Getenv("SEKAILINK_DNS_SERVERS"))
	_, resolvErr := os.Stat("/etc/resolv.conf")

	if serversEnv == "" && resolvErr == nil {
		return
	}

	var servers []string
	if serversEnv != "" {
		for _, s := range strings.Split(serversEnv, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !strings.Contains(s, ":") {
				s += ":53"
			}
			servers = append(servers, s)
		}
	}
	if len(servers) == 0 {
		servers = []string{"1.1.1.1:53", "8.8.8.8:53"}
	} else {
		for _, fallback := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
			dup := false
			for _, existing := range servers {
				if existing == fallback {
					dup = true
					break
				}
			}
			if !dup {
				servers = append(servers, fallback)
			}
		}
	}

	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 4 * time.Second}
			var lastErr error
			for _, srv := range servers {
				conn, err := d.DialContext(ctx, "udp", srv)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
}
