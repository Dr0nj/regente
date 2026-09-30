package runtimeprofile

import "testing"

func TestI08ExecutionLabGuard(t *testing.T) {
	if err := (Config{Profile: "production", ExecutionLab: true}).Validate(); err == nil {
		t.Fatal("laboratório habilitado em production")
	}
	if err := (Config{Profile: "development", ExecutionLab: true}).Validate(); err != nil {
		t.Fatal(err)
	}
}
