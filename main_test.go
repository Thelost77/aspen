package main

import "testing"

func TestRunHelp(t *testing.T) {
	if err := run([]string{"-h"}); err != nil {
		t.Fatalf("run(-h) = %v", err)
	}
}

func TestRunRejectsUnknownGraphicsProtocol(t *testing.T) {
	if err := run([]string{"--graphics", "invalid"}); err == nil {
		t.Fatal("run() accepted unknown graphics protocol")
	}
}

func TestRunVersion(t *testing.T) {
	if err := run([]string{"--version"}); err != nil {
		t.Fatalf("run(--version) = %v", err)
	}
}
