package network

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

// RunDiagnostics checks local IP, public IP, UPnP router external IP, and estimates CGNAT status.
func RunDiagnostics(ctx context.Context) models.NetworkDiagnostics {
	diag := models.NetworkDiagnostics{
		LocalIP: engine.DetectLocalLANIP(),
		Status:  "unknown",
	}

	publicIP := fetchPublicIP(ctx)
	diag.PublicIP = publicIP

	extIP, ok := ProbeUPnPExternalIP(ctx)
	diag.UPnPAvailable = ok
	diag.RouterExternalIP = extIP

	if extIP != "" {
		if isCGNATIP(extIP) || (publicIP != "" && extIP != publicIP) {
			diag.BehindCGNAT = true
			diag.Status = "cgnat_detected"
			diag.Recommendation = "CGNAT tespit edildi. Modemden port acmak dis baglanti saglamayacaktir; playit.gg tunelini kullanin."
			return diag
		}
		if publicIP != "" && extIP == publicIP {
			diag.BehindCGNAT = false
			diag.Status = "direct_ok"
			diag.Recommendation = "Public IP dogrudan erisilebilir gorunuyor. UPnP port acma veya playit.gg tunelini kullanabilirsiniz."
			return diag
		}
	}

	if publicIP == "" {
		diag.Recommendation = "Dis IP sorgulanamadi. Internet baglantinizi kontrol edin."
	} else {
		diag.BehindCGNAT = true
		diag.Status = "cgnat_detected"
		diag.Recommendation = "UPnP yanit vermedi veya CGNAT arkasindasiniz. En sorunsuz dis baglanti icin tek tikla playit.gg tuneli onerilir."
	}
	return diag
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
