package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func planFor(t *testing.T, raw string) *conversionPlan {
	t.Helper()
	d, err := inventory("synthetic.json", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return reviewPlan([]byte(raw), d, false)
}

func TestReviewCommandPreservesLiteralAndHierarchy(t *testing.T) {
	p := planFor(t, `{"demo":{"Type":"Folder","nested":{"Type":"SubFolder","task":{"Type":"Job:Command","Command":"  printf 'hello'  "}}}}`)
	if p.Executable || p.RecognizedJobs != 1 || p.ActionCandidates != 1 || p.BlockedJobs != 0 {
		t.Fatalf("%+v", p)
	}
	j := p.Jobs[0]
	if j.Action.Command != "  printf 'hello'  " || j.Action.JobType != "COMMAND" || j.Pointer != "/demo/nested/task" || len(j.Ancestors) != 2 || j.Ancestors[1] != "/demo/nested" {
		t.Fatalf("%+v", j)
	}
	if len(p.RequiredGates) == 0 {
		t.Fatal("missing qualification limits")
	}
}

func TestReviewInheritedAndUnknownPropertiesBlock(t *testing.T) {
	for _, raw := range []string{
		`{"demo":{"Type":"Folder","When":{"Months":["NONE"]},"task":{"Type":"Job:Command","Command":"echo ok"}}}`,
		`{"demo":{"Type":"Folder","Defaults":{"RunAs":"synthetic"},"task":{"Type":"Job:Command","Command":"echo ok"}}}`,
		`{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":"echo ok","Host":"synthetic"}}}`,
		`{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":"echo %%VALUE"}}}`,
		`{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":"echo ok"}},"Defaults":{"RunAs":"synthetic"}}`,
		`{"demo":{"Type":"Folder","task":{"Type":"Job:Dummy"}}}`,
		`{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":false}}}`,
		`{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":"echo ok","FutureProperty":null}}}`,
	} {
		p := planFor(t, raw)
		if p.RecognizedJobs != 1 || p.BlockedJobs != 1 || p.ActionCandidates != 0 || len(p.Jobs[0].Blockers) == 0 || p.Jobs[0].Action != nil {
			t.Fatalf("%s: %+v", raw, p)
		}
	}
}

func TestReviewArrayDuplicatesHaveDistinctIdentity(t *testing.T) {
	p := planFor(t, `{"Folders":[{"Type":"Folder","Name":"same","Jobs":[{"Type":"Job:Command","Name":"same","Command":"echo a"},{"Type":"Job:Command","Name":"same","Command":"echo b"}]},{"Type":"Folder","Name":"same","Jobs":[{"Type":"Job:Command","Name":"same","Command":"echo c"}]}]}`)
	if p.RecognizedJobs != 3 || p.ActionCandidates != 3 {
		t.Fatalf("%+v", p)
	}
	seen := map[string]bool{}
	for _, j := range p.Jobs {
		if seen[j.Pointer] {
			t.Fatal("collapsed duplicate name")
		}
		seen[j.Pointer] = true
	}
}

func TestReviewMalformedSiblingBlocksAllProposals(t *testing.T) {
	p := planFor(t, `{"demo":{"Type":"Folder","Jobs":{},"task":{"Type":"Job:Command","Command":"echo ok"}}}`)
	if p.ActionCandidates != 0 || p.BlockedJobs != 1 || len(p.Findings) == 0 {
		t.Fatalf("%+v", p)
	}
}

func TestReviewLargeUnknownNumberDoesNotLoseDocument(t *testing.T) {
	p := planFor(t, `{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":"echo ok","Future":1e999}}}`)
	if p.RecognizedJobs != 1 || p.BlockedJobs != 1 {
		t.Fatalf("%+v", p)
	}
}

func TestReviewNestedFolderIsNotSilentlyAccepted(t *testing.T) {
	p := planFor(t, `{"demo":{"Type":"Folder","invalid":{"Type":"Folder"},"task":{"Type":"Job:Command","Command":"echo ok"}}}`)
	if p.ActionCandidates != 0 || len(p.Findings) == 0 {
		t.Fatalf("%+v", p)
	}
}

func TestReviewDoesNotInterpretPayloadAsDefinition(t *testing.T) {
	p := planFor(t, `{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":"echo ok","Payload":{"Type":"Job:Command","Command":"never run"}}}}`)
	if p.RecognizedJobs != 1 || p.ActionCandidates != 0 {
		t.Fatalf("%+v", p)
	}
	found := false
	for _, f := range p.Findings {
		if f.Pointer == "/demo/task/Payload" && f.Code == "unresolved-definition-layout" {
			found = true
		}
	}
	if !found {
		t.Fatal("unresolved layout disappeared")
	}
}

func TestReviewEmptyFolderConstraintsAndDirectoryScope(t *testing.T) {
	p := planFor(t, `{"demo":{"Type":"Folder","When":{"Schedule":"Never"}}}`)
	if len(p.Constraints) != 1 || p.Constraints[0].Pointer != "/demo/When" {
		t.Fatalf("%+v", p)
	}
	raw := []byte(`{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":"echo ok"}}}`)
	d, err := inventory("demo.json", raw)
	if err != nil {
		t.Fatal(err)
	}
	p = reviewPlan(raw, d, true)
	if p.ActionCandidates != 0 || p.Jobs[0].Blockers[0].Code != "file-composition-unverified" {
		t.Fatalf("%+v", p)
	}
}

func TestReviewCLIContractAndPrivacyBoundary(t *testing.T) {
	file := filepath.Join(t.TempDir(), "demo.json")
	if err := os.WriteFile(file, []byte(`{"demo":{"Type":"Folder","task":{"Type":"Job:Command","Command":"echo PRIVATE_LITERAL"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"-in", file, "-source-version", "9.0.22.100"}, &out); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.Bytes(), []byte("PRIVATE_LITERAL")) || bytes.Contains(out.Bytes(), []byte(`"plan"`)) {
		t.Fatal("inventory default changed")
	}
	out.Reset()
	if err := run([]string{"-in", file, "-source-version", "9.0.22.100", "-plan"}, &out); err != nil {
		t.Fatal(err)
	}
	var result report
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Executable || result.Documents[0].Plan.ActionCandidates != 1 {
		t.Fatalf("%+v", result)
	}
	out.Reset()
	if run([]string{"-in", file, "-source-version", "9.0.22", "-plan"}, &out) == nil || out.Len() != 0 {
		t.Fatal("unqualified version accepted")
	}
}

func TestReviewRequiresArrayNamesAndNamedRoot(t *testing.T) {
	p := planFor(t, `{"Folders":[{"Type":"Folder","Jobs":[{"Type":"Job:Command","Command":"echo ok"}]}]}`)
	if p.ActionCandidates != 0 || p.BlockedJobs != 1 || len(p.Jobs[0].Blockers) != 2 {
		t.Fatalf("%+v", p)
	}
	for _, raw := range []string{`{"Type":"Folder"}`, `[{"Type":"Folder"}]`} {
		p = planFor(t, raw)
		if len(p.Findings) == 0 || p.ActionCandidates != 0 {
			t.Fatalf("%+v", p)
		}
	}
}
