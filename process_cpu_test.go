package main

import (
	"math"
	"testing"
	"time"
)

func TestProcessCPUSamplerUsesCPUAndWallClockDeltas(t *testing.T) {
	var sampler processCPUSampler
	start := time.Unix(100, 0)
	if value := sampler.sample(42, 10, start); value != 0 {
		t.Fatalf("first CPU sample = %v", value)
	}
	if value := sampler.sample(42, 11.5, start.Add(2*time.Second)); math.Abs(value-75) > 0.001 {
		t.Fatalf("second CPU sample = %v, want 75", value)
	}
}

func TestProcessCPUSamplerSeparatesProcessesAndResets(t *testing.T) {
	var sampler processCPUSampler
	start := time.Unix(100, 0)
	_ = sampler.sample(1, 5, start)
	_ = sampler.sample(2, 20, start)
	if value := sampler.sample(1, 6, start.Add(time.Second)); math.Abs(value-100) > 0.001 {
		t.Fatalf("PID 1 CPU = %v, want 100", value)
	}
	if value := sampler.sample(2, 20.5, start.Add(time.Second)); math.Abs(value-50) > 0.001 {
		t.Fatalf("PID 2 CPU = %v, want 50", value)
	}
	if value := sampler.sample(1, 0.25, start.Add(2*time.Second)); value != 0 {
		t.Fatalf("reset CPU sample = %v", value)
	}
}

func TestProcessCPUSamplerIgnoresConcurrentShortIntervalPolls(t *testing.T) {
	var sampler processCPUSampler
	start := time.Unix(100, 0)
	_ = sampler.sample(42, 10, start)
	if value := sampler.sample(42, 11, start.Add(time.Second)); math.Abs(value-100) > 0.001 {
		t.Fatalf("stable CPU sample = %v", value)
	}
	if value := sampler.sample(42, 11.1, start.Add(1100*time.Millisecond)); math.Abs(value-100) > 0.001 {
		t.Fatalf("short interval poll changed CPU sample to %v", value)
	}
	if value := sampler.sample(42, 12, start.Add(2*time.Second)); math.Abs(value-100) > 0.001 {
		t.Fatalf("next stable CPU sample = %v", value)
	}
}
