package main

import (
	"errors"
	"testing"
)

func TestShouldAutomaticallyStart(t *testing.T) {
	tests := []struct {
		name     string
		instance ServerInstance
		status   RuntimeStatus
		err      error
		want     bool
	}{
		{name: "enabled and stopped", instance: ServerInstance{StartOnBoot: true}, want: true},
		{name: "disabled", instance: ServerInstance{StartOnBoot: false}, want: false},
		{name: "already running", instance: ServerInstance{StartOnBoot: true}, status: RuntimeStatus{Running: true}, want: false},
		{name: "status failure", instance: ServerInstance{StartOnBoot: true}, err: errors.New("status failed"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldAutomaticallyStart(test.instance, test.status, test.err); got != test.want {
				t.Fatalf("shouldAutomaticallyStart() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestStoreWarningDeduplicatesAutomaticStartFailures(t *testing.T) {
	store := &Store{}
	store.AddWarning("auto-start failed")
	store.AddWarning("auto-start failed")
	if warnings := store.Warnings(); len(warnings) != 1 || warnings[0] != "auto-start failed" {
		t.Fatalf("warnings = %#v", warnings)
	}
}
