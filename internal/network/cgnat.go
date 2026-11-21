package network

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

// RunDiagnostics checks local IP, public IP, UPnP router external IP, CGNAT status, and tunnel RTT latencies.
func RunDiagnostics(ctx context.Context) models.NetworkDiagnostics {
	diag := models.NetworkDiagnostics{
		LocalIP: engine.DetectLocalLANIP(),
		Status:  "unknown",
	}

	var (
		wg         sync.WaitGroup
		publicIP   string
		extIP      string
		upnpOK     bool
		e4mcRegion string
		e4mcPing   int64
		playitPing int64
		borePing   int64
	)

	wg.Add(5)
	go func() {
		defer wg.Done()
		publicIP = fetchPublicIP(ctx)
	}()
	go func() {
		defer wg.Done()
		extIP, upnpOK = ProbeUPnPExternalIP(ctx)
	}()
	go func() {
		defer wg.Done()
		relay, err := fetchE4mcRelay(defaultE4mcBrokerURL)
		if err == nil {
			probed := probeE4mcCandidates(ctx, buildE4mcRelayCandidates(relay), 1500*time.Millisecond)
			for _, p := range probed {
				if p.rttMs > 0 && (e4mcPing == 0 || p.rttMs < e4mcPing) {
					e4mcPing = p.rttMs
					e4mcRegion = strings.ToUpper(strings.TrimSpace(p.relay.ID))
				}
			}
			if e4mcRegion == "" {
				e4mcRegion = strings.ToUpper(strings.TrimSpace(relay.ID))
			}
		}
		if e4mcPing <= 0 {
			e4mcPing = probeTCPPing(ctx, "broker.e4mc.link:443")
		}
	}()
	go func() {
		defer wg.Done()
		playitPing = probeTCPPing(ctx, "147.185.221.21:443")
		if playitPing <= 0 {
			playitPing = probeTCPPing(ctx, "playit.gg:443")
		}
	}()
	go func() {
		defer wg.Done()
		borePing = probeTCPPing(ctx, "bore.pub:7835")
	}()
	wg.Wait()

	diag.PublicIP = publicIP
	diag.UPnPAvailable = upnpOK
	diag.RouterExternalIP = extIP
	diag.E4mcRegion = e4mcRegion
	diag.E4mcPingMs = e4mcPing
	diag.PlayitPingMs = playitPing
	diag.BorePingMs = borePing

	if extIP != "" {
		if isCGNATIP(extIP) || (publicIP != "" && extIP != publicIP) {
			diag.BehindCGNAT = true
			diag.Status = "cgnat_detected"
			diag.Recommendation = "CGNAT tespit edildi. Java icin en dusuk pingli ve hesapsiz e4mc tunelini, Bedrock/Cross-Play icin playit.gg (Anycast) tunelini kullanin."
			return diag
		}
		if publicIP != "" && extIP == publicIP {
			diag.BehindCGNAT = false
			diag.Status = "direct_ok"
			diag.Recommendation = "Public IP dogrudan erisilebilir gorunuyor. UPnP port acabilir veya e4mc / playit.gg tunellerini kullanabilirsiniz."
			return diag
		}
	}

	if publicIP == "" {
		diag.Recommendation = "Dis IP sorgulanamadi. Internet baglantinizi kontrol edin."
	} else {
		diag.BehindCGNAT = true
		diag.Status = "cgnat_detected"
		diag.Recommendation = "UPnP yanit vermedi veya CGNAT arkasindasiniz. En dusuk ping icin e4mc (Java, hesapsiz) veya playit.gg (Anycast + Bedrock) tuneli onerilir."
	}
	return diag
}

func probeTCPPing(ctx context.Context, addr string) int64 {
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	d := net.Dialer{Timeout: 2 * time.Second}
	t0 := time.Now()
	c, err := d.DialContext(pctx, "tcp", addr)
	if err != nil {
		return 0
	}
	ms := time.Since(t0).Milliseconds()
	_ = c.Close()
	if ms < 1 {
		return 1
	}
	return ms
}

func fetchPublicIP(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func isCGNATIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	_, cgnatNet, _ := net.ParseCIDR("100.64.0.0/10")
	if cgnatNet != nil && cgnatNet.Contains(ip) {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	return false
}
