package runtimeprofile

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var configurationKey = regexp.MustCompile(`^REGENTE_[A-Z0-9_]+$`)

// LoadEnvironment reads data, never shell code. Explicit process environment wins.
func LoadEnvironment(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("configuration must be a regular file")
	}
	if err = CheckPrivateConfig(path, info); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.New("configuration is unreadable")
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !configurationKey.MatchString(key) || strings.ContainsRune(value, 0) {
			return errors.New("invalid configuration entry")
		}
		if _, duplicate := values[key]; duplicate {
			return errors.New("duplicate configuration key")
		}
		values[key] = value
	}
	for key, value := range values {
		if _, set := os.LookupEnv(key); !set {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

// Initialize writes an exclusively new file; secrets are never printed.
func Initialize(input io.Reader, output io.Writer, path string) error {
	reader := bufio.NewReader(input)
	fmt.Fprint(output, "Environment [local]: ")
	environment, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	environment = strings.TrimSpace(environment)
	if environment == "" {
		environment = "local"
	}
	if !environmentPattern.MatchString(environment) {
		return errors.New("invalid environment")
	}
	fmt.Fprint(output, "Local HTTP port [8080]: ")
	port, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	port = strings.TrimSpace(port)
	if port == "" {
		port = "8080"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("invalid port")
	}
	secret := make([]byte, 24)
	if _, err = rand.Read(secret); err != nil {
		return err
	}
	addr := net.JoinHostPort("127.0.0.1", port)
	content := fmt.Sprintf("# Private initial configuration. Remove bootstrap password after first login.\nREGENTE_PROFILE=production\nREGENTE_ENVIRONMENT=%s\nREGENTE_NETWORK_BOUNDARY=loopback\nREGENTE_ADDR=%s\nREGENTE_APP_URL=http://%s\nREGENTE_TOKEN=\nREGENTE_CONTROL_PLANE_EXECUTION=deny\nREGENTE_SERVER_AGENT=0\nREGENTE_BOOTSTRAP_USER=admin\nREGENTE_BOOTSTRAP_PASSWORD=%s\n", environment, addr, addr, hex.EncodeToString(secret))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("configuration target must be a new writable file")
	}
	success := false
	defer func() {
		f.Close()
		if !success {
			os.Remove(path)
		}
	}()
	if err = ProtectConfig(path); err != nil {
		return err
	}
	if _, err = f.WriteString(content); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	success = true
	fmt.Fprintln(output, "Configuration created. Read its bootstrap password locally, start with -config-env FILE, then remove the password after first login. Keep this file private.")
	return nil
}

// LocalAddress refuses DNS aliases, unspecified addresses and malformed ports.
func LocalAddress(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	n, e := strconv.Atoi(port)
	return ip != nil && ip.IsLoopback() && e == nil && n >= 0 && n <= 65535
}
