package engine

import (
	"os"
	"testing"
)

func TestComputeDeltaCPUPercent(t *testing.T) {
	// 3.0s CPU time across 1.0s wall clock on 8 cores = 37.5% (not 300%!)
	if got := computeDeltaCPUPercent(10.0, 13.0, 1.0, 8); got != 37.5 {
		t.Fatalf("expected 37.5%%, got %.1f%%", got)
	}
	// Clamped to 100% even if kernel delta slightly exceeds nominal wall*cores
	if got := computeDeltaCPUPercent(10.0, 22.0, 1.0, 8); got != 100.0 {
		t.Fatalf("expected clamped 100.0%%, got %.1f%%", got)
	}
	// Too small wall window or non-increasing CPU returns 0
	if got := computeDeltaCPUPercent(10.0, 10.5, 0.01, 8); got != 0 {
		t.Fatalf("expected 0 for tiny wall delta, got %.1f", got)
	}
	if got := computeDeltaCPUPercent(12.0, 11.0, 1.0, 8); got != 0 {
		t.Fatalf("expected 0 for non-increasing CPU time, got %.1f", got)
	}
}

func TestParseProcStatusVmRSSMBAndMemInfo(t *testing.T) {
	statusSample := "Name:\tjava\nState:\tS (sleeping)\nVmRSS:\t  524288 kB\nThreads:\t42\n"
	if got := parseProcStatusVmRSSMB(statusSample); got != 512.0 {
		t.Fatalf("expected VmRSS=512.0 MB, got %.1f MB", got)
	}

	meminfoSample := "MemTotal:       8388608 kB\nMemFree:         524288 kB\nMemAvailable:   3145728 kB\n"
	total, used, free, ok := parseProcMemInfoMB(meminfoSample)
	if !ok || total != 8192 || free != 3072 || used != 5120 {
		t.Fatalf("unexpected /proc/meminfo parse: ok=%v total=%d used=%d free=%d", ok, total, used, free)
	}
}

func TestProbeProcessMetricsSelf(t *testing.T) {
	pid := int32(os.Getpid())
	ClearProcessMetrics(pid)
	cpu1, mem1 := ProbeProcessMetrics(pid)
	if mem1 <= 0 {
		t.Fatalf("expected positive self RSS memory, got %.1f MB", mem1)
	}
	if cpu1 < 0 || cpu1 > 100 {
		t.Fatalf("expected bounded initial cpu in [0,100], got %.1f", cpu1)
	}
	cpu2, mem2 := ProbeProcessMetrics(pid)
	if mem2 <= 0 || cpu2 < 0 || cpu2 > 100 {
		t.Fatalf("expected valid subsequent probe metrics, got cpu=%.1f mem=%.1f", cpu2, mem2)
	}
	ClearProcessMetrics(pid)
}
