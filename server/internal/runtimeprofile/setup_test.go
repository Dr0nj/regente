package runtimeprofile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuidedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.env")
	var out bytes.Buffer
	if err := Initialize(strings.NewReader("lab\n18080\n"), &out, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if err := CheckPrivateConfig(path, info); err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "REGENTE_ADDR=127.0.0.1:18080") || !strings.Contains(text, "REGENTE_TOKEN=\n") || !strings.Contains(text, "REGENTE_SERVER_AGENT=0") {
		t.Fatal("unsafe generated defaults")
	}
	_, secret, _ := strings.Cut(text, "REGENTE_BOOTSTRAP_PASSWORD=")
	secret = strings.TrimSpace(secret)
	if len(secret) != 48 || strings.Contains(out.String(), secret) {
		t.Fatal("missing secret or disclosure")
	}
	if Initialize(strings.NewReader("\n\n"), &out, path) == nil {
		t.Fatal("overwrote config")
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(data, again) {
		t.Fatal("existing config modified")
	}
}

func TestConfigValidationBeforeEnvironmentMutation(t *testing.T) {
	for _, content := range []string{"REGENTE_SETUP_TEST=one\nREGENTE_SETUP_TEST=two", "REGENTE_SETUP_TEST=one\nOTHER=two", "REGENTE_SETUP_TEST=one\nREGENTE_BAD-KEY=x", "REGENTE_SETUP_TEST=one\nREGENTE_OTHER=bad\x00value"} {
		t.Run(content, func(t *testing.T) {
			t.Setenv("REGENTE_SETUP_TEST", "original")
			path := filepath.Join(t.TempDir(), "config")
			os.WriteFile(path, []byte(content), 0600)
			if e := ProtectConfig(path); e != nil {
				t.Fatal(e)
			}
			if LoadEnvironment(path) == nil {
				t.Fatal("accepted malformed config")
			}
			if os.Getenv("REGENTE_SETUP_TEST") != "original" {
				t.Fatal("partial mutation")
			}
		})
	}
	t.Setenv("REGENTE_SETUP_TEST", "explicit")
	path := filepath.Join(t.TempDir(), "config")
	os.WriteFile(path, []byte("REGENTE_SETUP_TEST=file"), 0600)
	if e := ProtectConfig(path); e != nil {
		t.Fatal(e)
	}
	if e := LoadEnvironment(path); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("REGENTE_SETUP_TEST") != "explicit" {
		t.Fatal("process env lost priority")
	}
}

func TestInitializeRejectsInvalidInputWithoutFile(t *testing.T) {
	for _, input := range []string{"bad env\n8080\n", "local\n0\n", "local\n65536\n"} {
		path := filepath.Join(t.TempDir(), "config")
		if Initialize(strings.NewReader(input), &bytes.Buffer{}, path) == nil {
			t.Fatal("accepted invalid input")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("created file for invalid input")
		}
	}
}
func TestLocalAddressBoundary(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "[::1]:8080", "127.0.0.2:0"} {
		if !LocalAddress(addr) {
			t.Fatal(addr)
		}
	}
	for _, addr := range []string{":8080", "0.0.0.0:8080", "localhost:8080", "192.168.1.1:8080", "127.0.0.1:65536"} {
		if LocalAddress(addr) {
			t.Fatal(addr)
		}
	}
}
