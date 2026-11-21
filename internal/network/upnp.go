package network

import (
	"context"
	"fmt"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

type portMapper interface {
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	AddPortMappingCtx(ctx context.Context, NewRemoteHost string, NewExternalPort uint16, NewProtocol string, NewInternalPort uint16, NewInternalClient string, NewEnabled bool, NewPortMappingDescription string, NewLeaseDuration uint32) error
	DeletePortMappingCtx(ctx context.Context, NewRemoteHost string, NewExternalPort uint16, NewProtocol string) error
}

func findPortMapper(ctx context.Context) (portMapper, error) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if clients, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway1.NewWANIPConnection1ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway1.NewWANPPPConnection1ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	return nil, fmt.Errorf("UPnP destekli modem/router bulunamadi")
}

func ProbeUPnPExternalIP(ctx context.Context) (string, bool) {
	mapper, err := findPortMapper(ctx)
	if err != nil {
		return "", false
	}
	ip, err := mapper.GetExternalIPAddressCtx(ctx)
	if err != nil {
		return "", true
	}
	return ip, true
}

func MapPortUPnP(ctx context.Context, port int, protocol string, desc string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("gecersiz port: %d", port)
	}
	mapper, err := findPortMapper(ctx)
	if err != nil {
		return err
	}
	localIP := engine.DetectLocalLANIP()
	return mapper.AddPortMappingCtx(ctx, "", uint16(port), protocol, uint16(port), localIP, true, desc, 0)
}

func UnmapPortUPnP(ctx context.Context, port int, protocol string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("gecersiz port: %d", port)
	}
	mapper, err := findPortMapper(ctx)
	if err != nil {
		return err
	}
	return mapper.DeletePortMappingCtx(ctx, "", uint16(port), protocol)
}
