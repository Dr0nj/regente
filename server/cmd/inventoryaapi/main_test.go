package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryPreservesStructureWithoutValues(t *testing.T) {
	raw := []byte(`{"demo":{"Type":"Folder","When":{"Months":["NONE"]},"Jobs":[{"Type":"Job:Command","Name":"same","Command":"PRIVATE_VALUE"},{"Type":"Job:Future","Name":"same","x/y~z":null}]}}`)
	doc, err := inventory("demo.json", raw)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(doc)
	if bytes.Contains(b, []byte("PRIVATE_VALUE")) {
		t.Fatal("scalar value leaked")
	}
	found := map[string]bool{}
	for _, n := range doc.Nodes {
		found[n.Pointer] = true
		if n.Status != "inventoried-unverified" {
			t.Fatal(n)
		}
	}
	for _, p := range []string{"/demo/When/Months/0", "/demo/Jobs/0/Name", "/demo/Jobs/1/Name", "/demo/Jobs/1/x~1y~0z"} {
		if !found[p] {
			t.Fatal("missing", p)
		}
	}
	if len(doc.SHA256) != 64 {
		t.Fatal("missing provenance")
	}
}
func TestInventoryRejectsLossyOrInvalidJSON(t *testing.T) {
	for _, raw := range []string{`{"x":1,"x":2}`, `{"x":{"a":1,"a":2}}`, `{} {}`, `{"Type":null}`, `{"Type":{}}`, `42`, `{"x":`, strings.Repeat("[", 130) + strings.Repeat("]", 130)} {
		if _, err := inventory("bad.json", []byte(raw)); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
func TestDirectoryAtomicDeterministicReport(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{".folder.json": `{"Type":"Folder"}`, "job.json": `{"job":{"Type":"Job:Dummy"}}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"-in", dir, "-source-version", "synthetic-v1"}
	var a, b bytes.Buffer
	if err := run(args, &a); err != nil {
		t.Fatal(err)
	}
	if err := run(args, &b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("nondeterministic")
	}
	var r report
	if err := json.Unmarshal(a.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Executable || len(r.Documents) != 2 || r.Documents[0].File != ".folder.json" {
		t.Fatal(r)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"a":`), 0600); err != nil {
		t.Fatal(err)
	}
	b.Reset()
	if err := run(args, &b); err == nil || b.Len() != 0 {
		t.Fatal("partial output on failure")
	}
}
func TestRequiredVersionAndSizeLimit(t *testing.T) {
	var out bytes.Buffer
	if run([]string{"-in", "missing"}, &out) == nil {
		t.Fatal("missing version accepted")
	}
	if _, err := inventory("large.json", bytes.Repeat([]byte(" "), maxBytes+1)); err == nil {
		t.Fatal("oversize accepted")
	}
}
