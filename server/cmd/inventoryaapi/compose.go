package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
)

const compositionContract = "regente-aapi-composition/v1"

type jobBinding struct {
	Name string `json:"name"`
	File string `json:"file"`
}
type folderBinding struct {
	Name     string       `json:"name"`
	Metadata string       `json:"metadata"`
	Jobs     []jobBinding `json:"jobs"`
}
type compositionManifest struct {
	Contract string          `json:"contract"`
	Folders  []folderBinding `json:"folders"`
}
type sourceBinding struct {
	File          string `json:"file"`
	SHA256        string `json:"sha256"`
	TargetPointer string `json:"targetPointer"`
}
type compositionResult struct {
	Contract       string          `json:"contract"`
	ManifestSHA256 string          `json:"manifestSha256"`
	Sources        []sourceBinding `json:"sources"`
	Document       document        `json:"document"`
}

func sourcePathValid(path string) bool {
	return path != "." && fs.ValidPath(path) && !strings.ContainsAny(path, "\\:\x00") && strings.HasSuffix(strings.ToLower(path), ".json")
}

// Source bytes are the same snapshot already inventoried by run. No filesystem
// path from the manifest is opened, no source is changed, and no job is emitted.
func composeFiles(path string, sources map[string][]byte) (*compositionResult, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("manifest must be a regular file, not a link")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if _, err = inventory("manifest", raw); err != nil {
		return nil, fmt.Errorf("invalid composition manifest: %w", err)
	}
	var manifest compositionManifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("invalid composition manifest schema")
	}
	if manifest.Contract != compositionContract || len(manifest.Folders) == 0 {
		return nil, fmt.Errorf("unsupported composition contract or empty folders")
	}
	result := &compositionResult{Contract: compositionContract, ManifestSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Sources: []sourceBinding{}}
	used := map[string]bool{}
	load := func(file, target string) (map[string]any, error) {
		if !sourcePathValid(file) {
			return nil, fmt.Errorf("source must be a relative slash-separated JSON path without traversal")
		}
		if used[file] {
			return nil, fmt.Errorf("source file bound more than once")
		}
		data, ok := sources[file]
		if !ok {
			return nil, fmt.Errorf("manifest source not found in input inventory")
		}
		// Keep validation local to this boundary as well as in the CLI.
		if _, err := inventory(file, data); err != nil {
			return nil, fmt.Errorf("invalid composition source: %w", err)
		}
		var obj map[string]any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&obj); err != nil || obj == nil {
			return nil, fmt.Errorf("source root must be a definition object")
		}
		used[file] = true
		result.Sources = append(result.Sources, sourceBinding{File: file, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), TargetPointer: target})
		return obj, nil
	}
	assembled := map[string]any{}
	for _, folder := range manifest.Folders {
		if strings.TrimSpace(folder.Name) == "" || folder.Name == "Folders" || folder.Name == "Type" {
			return nil, fmt.Errorf("invalid or reserved folder name")
		}
		if _, exists := assembled[folder.Name]; exists {
			return nil, fmt.Errorf("duplicate folder name")
		}
		ptr := pointerAt("", folder.Name)
		metadata, err := load(folder.Metadata, ptr)
		if err != nil {
			return nil, err
		}
		if objectType(metadata) != "Folder" {
			return nil, fmt.Errorf("metadata root must have Type Folder")
		}
		for k, v := range metadata {
			if k == "Name" || k == "Jobs" || k == "SubFolders" || isJob(objectType(v)) || isFolder(objectType(v)) {
				return nil, fmt.Errorf("metadata must not contain names, jobs or subfolders; declare jobs in the manifest")
			}
		}
		for _, job := range folder.Jobs {
			if strings.TrimSpace(job.Name) == "" || job.Name == "Name" || job.Name == "Jobs" || job.Name == "SubFolders" {
				return nil, fmt.Errorf("invalid or reserved job name")
			}
			if _, exists := metadata[job.Name]; exists {
				return nil, fmt.Errorf("job name collides with metadata or another job")
			}
			obj, err := load(job.File, pointerAt(ptr, job.Name))
			if err != nil {
				return nil, err
			}
			if !isJob(objectType(obj)) {
				return nil, fmt.Errorf("job source must have a Job: type at its root")
			}
			if _, exists := obj["Name"]; exists {
				return nil, fmt.Errorf("job source Name conflicts with explicit manifest naming")
			}
			metadata[job.Name] = obj
		}
		assembled[folder.Name] = metadata
	}
	if len(used) != len(sources) {
		return nil, fmt.Errorf("input contains JSON files not declared in the manifest")
	}
	// Encoding sorts map keys. Binding order never silently changes semantics.
	combined, err := json.Marshal(assembled)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize composition")
	}
	doc, err := inventory("<composition>", combined)
	if err != nil {
		return nil, fmt.Errorf("invalid combined document: %w", err)
	}
	doc.Plan = reviewPlan(combined, doc, false)
	result.Document = doc
	sort.Slice(result.Sources, func(i, j int) bool { return result.Sources[i].File < result.Sources[j].File })
	return result, nil
}
