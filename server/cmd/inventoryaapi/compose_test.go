package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func manifestFile(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func basicManifest() string {
	return `{"contract":"regente-aapi-composition/v1","folders":[{"name":"demo","metadata":"demo/.folder.json","jobs":[{"name":"task","file":"demo/task.json"}]}]}`
}
func basicSources() map[string][]byte {
	return map[string][]byte{
		"demo/.folder.json": []byte(`{"Type":"Folder"}`),
		"demo/task.json":    []byte(`{"Type":"Job:Command","Command":"echo synthetic"}`),
	}
}

func TestCompositionExplicitBindingAndProvenance(t *testing.T) {
	sources := basicSources()
	result, err := composeFiles(manifestFile(t, basicManifest()), sources)
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Plan.ActionCandidates != 1 || result.Document.Plan.Executable || len(result.Sources) != 2 {
		t.Fatalf("%+v", result)
	}
	if result.Document.Plan.Jobs[0].Pointer != "/demo/task" {
		t.Fatal("wrong hierarchy")
	}
	for _, s := range result.Sources {
		d, err := inventory(s.File, sources[s.File])
		if err != nil {
			t.Fatal(err)
		}
		if s.SHA256 != d.SHA256 {
			t.Fatal("source hash mismatch")
		}
		for _, n := range d.Nodes {
			found := false
			for _, target := range result.Document.Nodes {
				if target.Pointer == s.TargetPointer+n.Pointer {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("source pointer lost: %s%s", s.TargetPointer, n.Pointer)
			}
		}
	}
	if string(sources["demo/.folder.json"]) != `{"Type":"Folder"}` {
		t.Fatal("source mutated")
	}
}

func TestCompositionPropagatesMetadataRestrictions(t *testing.T) {
	sources := basicSources()
	sources["demo/.folder.json"] = []byte(`{"Type":"Folder","When":{"Schedule":"Never"},"Variables":[{"value":"%%ODATE"}]}`)
	r, err := composeFiles(manifestFile(t, basicManifest()), sources)
	if err != nil {
		t.Fatal(err)
	}
	if r.Document.Plan.ActionCandidates != 0 || r.Document.Plan.BlockedJobs != 1 {
		t.Fatalf("%+v", r.Document.Plan)
	}
	if len(r.Document.Plan.Jobs[0].Blockers) != 2 {
		t.Fatal("metadata restrictions lost")
	}
}

func TestCompositionRejectsAmbiguousSources(t *testing.T) {
	for name, mutate := range map[string]func(map[string][]byte){
		"unlisted": func(s map[string][]byte) { s["extra.json"] = []byte(`{}`) },
		"missing":  func(s map[string][]byte) { delete(s, "demo/task.json") },
		"embedded-job": func(s map[string][]byte) {
			s["demo/.folder.json"] = []byte(`{"Type":"Folder","hidden":{"Type":"Job:Dummy"}}`)
		},
		"metadata-collision": func(s map[string][]byte) { s["demo/.folder.json"] = []byte(`{"Type":"Folder","task":"metadata"}`) },
		"named-job": func(s map[string][]byte) {
			s["demo/task.json"] = []byte(`{"Type":"Job:Command","Name":"other","Command":"echo ok"}`)
		},
		"wrong-root": func(s map[string][]byte) { s["demo/task.json"] = []byte(`{"wrapped":{"Type":"Job:Dummy"}}`) },
		"duplicate-key": func(s map[string][]byte) {
			s["demo/task.json"] = []byte(`{"Type":"Job:Command","Command":"a","Command":"b"}`)
		},
		"subfolders": func(s map[string][]byte) { s["demo/.folder.json"] = []byte(`{"Type":"Folder","SubFolders":[]}`) },
	} {
		t.Run(name, func(t *testing.T) {
			s := basicSources()
			mutate(s)
			if _, err := composeFiles(manifestFile(t, basicManifest()), s); err == nil {
				t.Fatal("ambiguous composition accepted")
			}
		})
	}
}

func TestCompositionRejectsManifestMistakes(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(basicManifest(), `"contract":`, `"unknown":true,"contract":`, 1),
		strings.Replace(basicManifest(), `"contract":`, `"contract":"other","contract":`, 1),
		strings.Replace(basicManifest(), `"demo/task.json"`, `"../task.json"`, 1),
		strings.Replace(basicManifest(), `"demo/task.json"`, `"C:/task.json"`, 1),
		strings.Replace(basicManifest(), `"demo/task.json"`, `"demo/.folder.json"`, 1),
		strings.Replace(basicManifest(), `"name":"task"`, `"name":"Type"`, 1),
		strings.Replace(basicManifest(), `"name":"demo"`, `"name":"Folders"`, 1),
		strings.Replace(basicManifest(), `"name":"task","file":"demo/task.json"`, `"name":"task","file":"demo/task.json"},{"name":"task","file":"demo/task.json"`, 1),
		`{"contract":"regente-aapi-composition/v1","folders":[]}`,
	} {
		if _, err := composeFiles(manifestFile(t, raw), basicSources()); err == nil {
			t.Fatalf("accepted invalid manifest %s", raw)
		}
	}
}

func TestCompositionPathContract(t *testing.T) {
	for _, path := range []string{"../a.json", "/a.json", "a\\b.json", "a:b.json", "a/./b.json", "a//b.json", "a\x00.json", ".", "a.txt"} {
		if sourcePathValid(path) {
			t.Errorf("accepted %q", path)
		}
	}
	if !sourcePathValid("demo/.folder.json") {
		t.Fatal("valid path rejected")
	}
}

func TestCompositionCLIIsAtomicAndLeavesFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "demo"), 0700); err != nil {
		t.Fatal(err)
	}
	sources := basicSources()
	for file, raw := range sources {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(file)), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifest, []byte(basicManifest()), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-in", dir, "-source-version", "9.0.22.100", "-plan", "-compose", manifest}
	var a, b bytes.Buffer
	if err := run(args, &a); err != nil {
		t.Fatal(err)
	}
	if err := run(args, &b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("nondeterministic composition")
	}
	var result report
	if err := json.Unmarshal(a.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Executable || len(result.Documents) != 2 || result.Composition.Document.Plan.ActionCandidates != 1 {
		t.Fatal("invalid composed report")
	}
	for file, raw := range sources {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatal("source changed")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	a.Reset()
	if err := run(args, &a); err == nil || a.Len() != 0 {
		t.Fatal("partial report on composition error")
	}
	a.Reset()
	if run([]string{"-in", dir, "-source-version", "9.0.22.100", "-compose", manifest}, &a) == nil {
		t.Fatal("composition without plan")
	}
}

func TestCompositionBindingOrderDoesNotChangeDocument(t *testing.T) {
	sources := basicSources()
	sources["demo/second.json"] = []byte(`{"Type":"Job:Command","Command":"echo second"}`)
	first := strings.Replace(basicManifest(), `"jobs":[`, `"jobs":[{"name":"second","file":"demo/second.json"},`, 1)
	second := strings.Replace(basicManifest(), `"jobs":[{"name":"task","file":"demo/task.json"}]`, `"jobs":[{"name":"task","file":"demo/task.json"},{"name":"second","file":"demo/second.json"}]`, 1)
	a, err := composeFiles(manifestFile(t, first), sources)
	if err != nil {
		t.Fatal(err)
	}
	b, err := composeFiles(manifestFile(t, second), sources)
	if err != nil {
		t.Fatal(err)
	}
	if a.Document.SHA256 != b.Document.SHA256 || a.ManifestSHA256 == b.ManifestSHA256 {
		t.Fatal("document or manifest provenance incorrect")
	}
}

func TestCompositionFolderNamesAndEscapedPointers(t *testing.T) {
	sources := basicSources()
	sources["other/.folder.json"] = []byte(`{"Type":"Folder"}`)
	sources["other/task.json"] = []byte(`{"Type":"Job:Command","Command":"echo other"}`)
	manifest := compositionManifest{Contract: compositionContract, Folders: []folderBinding{
		{Name: "demo", Metadata: "demo/.folder.json", Jobs: []jobBinding{{Name: "task", File: "demo/task.json"}}},
		{Name: "other/~", Metadata: "other/.folder.json", Jobs: []jobBinding{{Name: "task", File: "other/task.json"}}},
	}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	r, err := composeFiles(manifestFile(t, string(raw)), sources)
	if err != nil {
		t.Fatal(err)
	}
	if r.Document.Plan.ActionCandidates != 2 || r.Document.Plan.Jobs[1].Pointer != "/other~1~0/task" {
		t.Fatalf("%+v", r.Document.Plan)
	}
	manifest.Folders[1].Name = "demo"
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := composeFiles(manifestFile(t, string(raw)), sources); err == nil {
		t.Fatal("duplicate folder accepted")
	}
}
