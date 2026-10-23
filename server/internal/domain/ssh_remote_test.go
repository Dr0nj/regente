package domain

import "testing"

func TestRemoteSSHSchemaRequiresExplicitTrust(t *testing.T) {
	p := map[string]interface{}{"host": "remote", "user": "runner", "command": "/jobs/run.sh", "keyPath": "/private/key", "knownHostsPath": "/private/known_hosts", "strictHostKey": "yes", "port": 2222}
	if err := vac("SSH_REMOTE", p); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"host", "user", "command", "keyPath", "knownHostsPath", "strictHostKey"} {
		old := p[field]
		delete(p, field)
		if vac("SSH_REMOTE", p) == nil {
			t.Fatalf("accepted missing %s", field)
		}
		p[field] = old
	}
	p["strictHostKey"] = "no"
	if vac("SSH_REMOTE", p) == nil {
		t.Fatal("accepted insecure host keys")
	}
}
