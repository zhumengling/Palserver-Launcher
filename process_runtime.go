package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// processRuntime isolates platform-specific process and host inspection from the
// launcher services. A Linux agent can provide the same contract without
// changing server lifecycle, capability, guardian, or UI code.
type processRuntime interface {
	FindServerProcess(ServerInstance) (serverProcessSnapshot, bool, error)
	HostResources() (HostResources, error)
	TCPListenerOwner(port int) (pid int, found bool, err error)
}

type serverProcessSnapshot struct {
	PID        int
	Path       string
	CPUSeconds float64
	CPUPercent float64
	MemoryMB   float64
	StartedAt  time.Time
}

type processCPUSample struct {
	CPUSeconds float64
	SampledAt  time.Time
	Percent    float64
}

type processCPUSampler struct {
	mu      sync.Mutex
	samples map[int]processCPUSample
}

func (sampler *processCPUSampler) sample(pid int, cpuSeconds float64, sampledAt time.Time) float64 {
	if pid <= 0 || cpuSeconds < 0 || sampledAt.IsZero() {
		return 0
	}
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	if sampler.samples == nil {
		sampler.samples = map[int]processCPUSample{}
	}
	previous, found := sampler.samples[pid]
	for otherPID, sample := range sampler.samples {
		if otherPID != pid && sampledAt.Sub(sample.SampledAt) > 10*time.Minute {
			delete(sampler.samples, otherPID)
		}
	}
	if !found || cpuSeconds < previous.CPUSeconds || !sampledAt.After(previous.SampledAt) {
		sampler.samples[pid] = processCPUSample{CPUSeconds: cpuSeconds, SampledAt: sampledAt}
		return 0
	}
	elapsed := sampledAt.Sub(previous.SampledAt)
	if elapsed < 500*time.Millisecond {
		return previous.Percent
	}
	percent := (cpuSeconds - previous.CPUSeconds) / elapsed.Seconds() * 100
	maximum := float64(max(1, runtime.NumCPU())) * 100
	if percent < 0 {
		return 0
	}
	if percent > maximum {
		percent = maximum
	}
	sampler.samples[pid] = processCPUSample{CPUSeconds: cpuSeconds, SampledAt: sampledAt, Percent: percent}
	return percent
}

var defaultProcessRuntime processRuntime = newPlatformProcessRuntime()

func serverProcessPathMatches(instance ServerInstance, executablePath string) bool {
	executablePath = filepath.Clean(executablePath)
	target := filepath.Clean(instance.Executable)
	if strings.EqualFold(executablePath, target) {
		return true
	}
	root := filepath.Clean(instance.RootPath)
	if root == "." || root == "" {
		root = filepath.Clean(filepath.Dir(target))
	}
	relative, err := filepath.Rel(root, executablePath)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
