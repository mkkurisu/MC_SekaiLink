package engine

import (
	"fmt"
	"math"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type cpuSample struct {
	totalSec float64
	at       time.Time
}

var (
	pidSamplesMu sync.Mutex
	pidSamples   = make(map[int32]cpuSample)
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

func ClearProcessMetrics(pid int32) {
	if pid <= 0 {
		return
	}
	pidSamplesMu.Lock()
	delete(pidSamples, pid)
	pidSamplesMu.Unlock()
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
		ov.CPUPercent = clampPercent(pct[0])
	}
	if vm, err := mem.VirtualMemory(); err == nil && vm != nil && vm.Total > 0 {
		ov.RAMTotalMB = vm.Total / (1024 * 1024)
		ov.RAMUsedMB = vm.Used / (1024 * 1024)
		ov.RAMFreeMB = vm.Available / (1024 * 1024)
	} else if runtime.GOOS == "linux" {
		if total, used, free, ok := readProcMemInfoMB(); ok {
			ov.RAMTotalMB = total
			ov.RAMUsedMB = used
			ov.RAMFreeMB = free
		}
	}
	if dataDir != "" {
		if du, err := disk.Usage(dataDir); err == nil {
			ov.DiskTotalGB = math.Round((float64(du.Total)/(1024*1024*1024))*100) / 100
			ov.DiskFreeGB = math.Round((float64(du.Free)/(1024*1024*1024))*100) / 100
		}
	}
	return ov
}

// ProbeProcessMetrics measures normalized [0, 100] CPU percentage using per-PID user+system deltas
// and RSS memory in MB (with Linux/Android /proc/<pid>/status fallback).
func ProbeProcessMetrics(pid int32) (cpuPct float64, memMB float64) {
	if pid <= 0 {
		return 0, 0
	}
	p, err := process.NewProcess(pid)
	if err != nil {
		return 0, 0
	}

	// p.Times() reads /proc/<pid>/stat on Linux/Android and works inside Android SELinux sandboxes
	// where global /proc/stat access is blocked.
	if times, err := p.Times(); err == nil && times != nil {
		curTotal := times.User + times.System
		now := time.Now()

		pidSamplesMu.Lock()
		prev, ok := pidSamples[pid]
		pidSamples[pid] = cpuSample{totalSec: curTotal, at: now}
		pidSamplesMu.Unlock()

		if ok {
			cpuPct = computeDeltaCPUPercent(prev.totalSec, curTotal, now.Sub(prev.at).Seconds(), runtime.NumCPU())
		}
	}

	if mi, err := p.MemoryInfo(); err == nil && mi != nil && mi.RSS > 0 {
		memMB = math.Round((float64(mi.RSS)/(1024*1024))*10) / 10
	} else if runtime.GOOS == "linux" {
		memMB = readProcStatusVmRSSMB(pid)
	}
	return cpuPct, memMB
}

func computeDeltaCPUPercent(prevTotalSec, curTotalSec, deltaWallSec float64, cores int) float64 {
	if deltaWallSec < 0.05 || curTotalSec <= prevTotalSec {
		return 0
	}
	if cores < 1 {
		cores = 1
	}
	pct := ((curTotalSec - prevTotalSec) / deltaWallSec) * 100.0 / float64(cores)
	return clampPercent(pct)
}

func clampPercent(pct float64) float64 {
	if math.IsNaN(pct) || math.IsInf(pct, 0) || pct <= 0 {
		return 0
	}
	if pct >= 100 {
		return 100
	}
	return math.Round(pct*10) / 10
}

func readProcStatusVmRSSMB(pid int32) float64 {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	return parseProcStatusVmRSSMB(string(raw))
}

func parseProcStatusVmRSSMB(statusText string) float64 {
	for _, line := range strings.Split(statusText, "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "VmRSS:"))
		if len(fields) == 0 {
			return 0
		}
		kb, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || kb <= 0 {
			return 0
		}
		return math.Round((kb/1024.0)*10) / 10
	}
	return 0
}

func readProcMemInfoMB() (totalMB, usedMB, freeMB uint64, ok bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, false
	}
	return parseProcMemInfoMB(string(raw))
}

func parseProcMemInfoMB(meminfoText string) (totalMB, usedMB, freeMB uint64, ok bool) {
	var totalKB, availKB, freeKB uint64
	hasAvail := false
	for _, line := range strings.Split(meminfoText, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			totalKB = val
		case "MemAvailable:":
			availKB = val
			hasAvail = true
		case "MemFree:":
			freeKB = val
		}
	}
	if totalKB == 0 {
		return 0, 0, 0, false
	}
	if !hasAvail {
		availKB = freeKB
	}
	if availKB > totalKB {
		availKB = totalKB
	}
	totalMB = totalKB / 1024
	freeMB = availKB / 1024
	usedMB = (totalKB - availKB) / 1024
	return totalMB, usedMB, freeMB, true
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
