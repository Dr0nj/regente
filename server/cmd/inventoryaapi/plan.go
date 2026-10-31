package main

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

const reviewContract = "regente-aapi-review/9.0.22.100/v1"

type finding struct {
	Pointer string `json:"pointer"`
	Code    string `json:"code"`
}
type actionProposal struct {
	JobType string `json:"jobType"`
	Command string `json:"command"`
}
type jobPlan struct {
	Pointer    string          `json:"pointer"`
	Name       string          `json:"name"`
	SourceType string          `json:"sourceType"`
	Ancestors  []string        `json:"ancestors"`
	Status     string          `json:"status"`
	Blockers   []finding       `json:"blockers"`
	Action     *actionProposal `json:"actionProposal,omitempty"`
}
type conversionPlan struct {
	Contract         string    `json:"contract"`
	Executable       bool      `json:"executable"`
	Jobs             []jobPlan `json:"jobs"`
	Findings         []finding `json:"findings"`
	Constraints      []finding `json:"constraints"`
	RequiredGates    []string  `json:"requiredGates"`
	RecognizedJobs   int       `json:"recognizedJobs"`
	ActionCandidates int       `json:"actionCandidates"`
	BlockedJobs      int       `json:"blockedJobs"`
}

func pointerAt(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func objectType(v any) string { m, _ := v.(map[string]any); s, _ := m["Type"].(string); return s }
func isFolder(t string) bool  { return t == "Folder" || t == "SubFolder" }
func isJob(t string) bool     { return strings.HasPrefix(t, "Job:") }
func keys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}

// This is a review model, not domain.JobDefinition. Even action candidates have
// no executable output: schedule, identity and runtime policy are not qualified.
func reviewPlan(raw []byte, doc document, directory bool) *conversionPlan {
	p := &conversionPlan{Contract: reviewContract, Jobs: []jobPlan{}, Findings: []finding{}, Constraints: []finding{}, RequiredGates: []string{"source-runtime-equivalence", "scheduling-and-folder-lifecycle", "execution-identity-and-policy", "workspace-conversion-review"}}
	var root any
	// inventory has already rejected malformed JSON and duplicate keys.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&root) != nil {
		p.Findings = append(p.Findings, finding{"", "invalid-review-document"})
		return p
	}
	global := []finding{}
	if directory {
		global = append(global, finding{"", "file-composition-unverified"})
	}
	seen := map[string]bool{}
	var visit func(any, string, string, []string, []finding)
	visit = func(value any, ptr, name string, ancestors []string, inherited []finding) {
		m, ok := value.(map[string]any)
		if !ok {
			p.Findings = append(p.Findings, finding{ptr, "expected-definition-object"})
			return
		}
		typ := objectType(m)
		if !isFolder(typ) && !isJob(typ) {
			p.Findings = append(p.Findings, finding{ptr, "unsupported-definition-type"})
			return
		}
		seen[ptr] = true
		blocks := append([]finding{}, inherited...)
		if name == "" {
			name, _ = m["Name"].(string)
			if strings.TrimSpace(name) == "" {
				blocks = append(blocks, finding{pointerAt(ptr, "Name"), "missing-array-name"})
			}
		} else if _, exists := m["Name"]; exists {
			blocks = append(blocks, finding{pointerAt(ptr, "Name"), "name-in-named-object"})
		}
		for _, k := range keys(m) {
			if k == "Type" || k == "Name" {
				continue
			}
			if isFolder(typ) {
				if k == "Jobs" || k == "SubFolders" {
					continue
				}
				if ct := objectType(m[k]); isFolder(ct) || isJob(ct) {
					continue
				}
			} else if k == "Command" && typ == "Job:Command" {
				continue
			}
			// No default or inherited property is silently discarded, including
			// metadata that has no lossless target representation yet.
			blocks = append(blocks, finding{pointerAt(ptr, k), "unmapped-property"})
			if isFolder(typ) {
				p.Constraints = append(p.Constraints, finding{pointerAt(ptr, k), "unmapped-property"})
			}
		}
		if isJob(typ) {
			j := jobPlan{Pointer: ptr, Name: name, SourceType: typ, Ancestors: append([]string{}, ancestors...), Status: "blocked", Blockers: blocks}
			if typ != "Job:Command" {
				j.Blockers = append(j.Blockers, finding{pointerAt(ptr, "Type"), "unsupported-job-type"})
			} else {
				cmd, ok := m["Command"].(string)
				if !ok || strings.TrimSpace(cmd) == "" {
					j.Blockers = append(j.Blockers, finding{pointerAt(ptr, "Command"), "invalid-command"})
				} else if strings.Contains(cmd, "%%") {
					j.Blockers = append(j.Blockers, finding{pointerAt(ptr, "Command"), "variable-resolution-unverified"})
				} else if len(j.Blockers) == 0 {
					j.Status = "action-candidate-review-only"
					j.Action = &actionProposal{JobType: "COMMAND", Command: cmd}
				}
			}
			p.Jobs = append(p.Jobs, j)
			return
		}
		parents := append(append([]string{}, ancestors...), ptr)
		for _, k := range keys(m) {
			child := pointerAt(ptr, k)
			if k == "Jobs" || k == "SubFolders" {
				list, ok := m[k].([]any)
				if !ok {
					p.Findings = append(p.Findings, finding{child, "expected-definition-array"})
					continue
				}
				for i, v := range list {
					at := pointerAt(child, strconv.Itoa(i))
					ct := objectType(v)
					if (k == "Jobs" && !isJob(ct)) || (k == "SubFolders" && ct != "SubFolder") {
						p.Findings = append(p.Findings, finding{at, "invalid-array-member-type"})
						continue
					}
					visit(v, at, "", parents, blocks)
				}
			} else if ct := objectType(m[k]); isFolder(ct) || isJob(ct) {
				if ct == "Folder" {
					p.Findings = append(p.Findings, finding{child, "nested-folder-requires-subfolder"})
				} else {
					visit(m[k], child, k, parents, blocks)
				}
			}
		}
	}
	m, ok := root.(map[string]any)
	if !ok || objectType(root) != "" {
		p.Findings = append(p.Findings, finding{"", "named-root-container-required"})
	} else {
		// Root properties such as Defaults affect every descendant. They must
		// block proposals even when they sort after a folder in the document.
		for _, k := range keys(m) {
			if k != "Folders" && !isFolder(objectType(m[k])) {
				global = append(global, finding{pointerAt("", k), "unmapped-root-property"})
			}
		}
		for _, k := range keys(m) {
			ptr := pointerAt("", k)
			if k == "Folders" {
				list, ok := m[k].([]any)
				if !ok {
					p.Findings = append(p.Findings, finding{ptr, "expected-definition-array"})
					continue
				}
				for i, v := range list {
					at := pointerAt(ptr, strconv.Itoa(i))
					if objectType(v) != "Folder" {
						p.Findings = append(p.Findings, finding{at, "invalid-array-member-type"})
						continue
					}
					visit(v, at, "", nil, global)
				}
			} else if objectType(m[k]) == "Folder" {
				visit(m[k], ptr, k, nil, global)
			} else {
				p.Findings = append(p.Findings, finding{ptr, "unmapped-root-property"})
			}
		}
	}
	// A Job-like object in arbitrary payload data is not automatically a job.
	// Keep it visible as an unresolved layout instead of inventing a hierarchy.
	for _, n := range doc.Nodes {
		if (isJob(n.DeclaredType) || isFolder(n.DeclaredType)) && !seen[n.Pointer] {
			p.Findings = append(p.Findings, finding{n.Pointer, "unresolved-definition-layout"})
		}
	}
	// Any unresolved document layout blocks ALL action proposals: a sibling
	// may contain an inherited constraint or external dependency.
	if len(p.Findings) > 0 {
		for i := range p.Jobs {
			j := &p.Jobs[i]
			j.Status = "blocked"
			j.Action = nil
			j.Blockers = append(j.Blockers, finding{"", "document-layout-unverified"})
		}
	}
	for _, j := range p.Jobs {
		if j.Action != nil {
			p.ActionCandidates++
		} else {
			p.BlockedJobs++
		}
	}
	p.Constraints = append(p.Constraints, global...)
	p.RecognizedJobs = len(p.Jobs)
	return p
}
