package engine

import (
	"math"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

func DetectPlatformMode() string {
	if os.Getenv("SEKAILINK_PLATFORM") == "android" || os.Getenv("ANDROID_ROOT") != "" {
		return "android"
	}
	if os.Getenv("SEKAILINK_MODE") == "desktop" {
		return "desktop"
	}
	return "headless"
}

func CollectSystemOverview(dataDir string, javaInfo JavaInfo) models.SystemOverview {
	ov := models.SystemOverview{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		PlatformMode: DetectPlatformMode(),
		CPUCores:     runtime.NumCPU(),
		JavaDetected: javaInfo.Detected,
		JavaVersion:  javaInfo.Version,
		JavaPath:     javaInfo.Path,
		LocalIP:      DetectLocalLANIP(),
	}

	if info, err := cpu.Info(); err == nil && len(info) > 0 {
		ov.CPUModel = info[0].ModelName
	}
	if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
		ov.CPUPercent = math.Round(pct[0]*10) / 10
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		ov.RAMTotalMB = vm.Total / (1024 * 1024)
		ov.RAMUsedMB = vm.Used / (1024 * 1024)
		ov.RAMFreeMB = vm.Available / (1024 * 1024)
	}
	if dataDir != "" {
		if du, err := disk.Usage(dataDir); err == nil {
			ov.DiskTotalGB = math.Round((float64(du.Total)/(1024*1024*1024))*100) / 100
			ov.DiskFreeGB = math.Round((float64(du.Free)/(1024*1024*1024))*100) / 100
		}
	}
	return ov
}

func ProbeProcessMetrics(pid int32) (cpuPct float64, memMB float64) {
	if pid <= 0 {
		return 0, 0
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return 0, 0
	}
	if c, err := p.CPUPercent(); err == nil {
		cpuPct = math.Round(c*10) / 10
	}
	if mi, err := p.MemoryInfo(); err == nil && mi != nil {
		memMB = math.Round((float64(mi.RSS)/(1024*1024))*10) / 10
	}
	return cpuPct, memMB
}

// DetectLocalLANIP finds the preferred non-loopback LAN IPv4 address.
func DetectLocalLANIP() string {
	conn, err := net.Dial("udp", "1.1.1.1:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP.To4() != nil {
			return addr.IP.String()
		}
	}
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil {
					ipStr := ipNet.IP.String()
					if !strings.HasPrefix(ipStr, "169.254.") {
						return ipStr
					}
				}
			}
		}
	}
	return "127.0.0.1"
}
