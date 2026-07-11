//go:build windows

package singleinstance

import "testing"

func TestAcquirePreventsSecondInstance(t *testing.T) {
	first, alreadyRunning, err := Acquire("DomainVPNRouterTestSingleInstance")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if alreadyRunning {
		t.Fatalf("first acquire reported already running")
	}
	defer first.Release()

	second, alreadyRunning, err := Acquire("DomainVPNRouterTestSingleInstance")
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if second != nil {
		defer second.Release()
	}
	if !alreadyRunning {
		t.Fatalf("second acquire did not report already running")
	}
}
