// inventoryaapi inventories JSON definitions without creating executable jobs.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxBytes = 16 << 20

type node struct {
	Pointer      string `json:"pointer"`
	Kind         string `json:"kind"`
	DeclaredType string `json:"declaredType,omitempty"`
	Status       string `json:"status"`
}
type document struct {
	File   string          `json:"file"`
	SHA256 string          `json:"sha256"`
	Nodes  []node          `json:"nodes"`
	Plan   *conversionPlan `json:"plan,omitempty"`
}
type report struct {
	Format        string     `json:"format"`
	SourceVersion string     `json:"sourceVersion"`
	Executable    bool       `json:"executable"`
	Documents     []document `json:"documents"`
}

// Token parsing rejects duplicate keys instead of silently overwriting them.
// Values are deliberately absent from the report; names/pointers remain private.
func readNode(d *json.Decoder, pointer string, depth int, nodes *[]node) error {
	if depth > 128 {
		return fmt.Errorf("nesting exceeds 128 levels")
	}
	t, err := d.Token()
	if err != nil {
		return fmt.Errorf("invalid JSON at byte %d", d.InputOffset())
	}
	n := node{Pointer: pointer, Status: "inventoried-unverified"}
	switch v := t.(type) {
	case json.Delim:
		if v != '{' && v != '[' {
			return fmt.Errorf("unexpected delimiter")
		}
		n.Kind = "object"
		if v == '[' {
			n.Kind = "array"
		}
		idx := len(*nodes)
		*nodes = append(*nodes, n)
		seen := map[string]bool{}
		for i := 0; d.More(); i++ {
			key := strconv.Itoa(i)
			if v == '{' {
				kt, e := d.Token()
				if e != nil {
					return fmt.Errorf("invalid object key")
				}
				var ok bool
				key, ok = kt.(string)
				if !ok || seen[key] {
					return fmt.Errorf("invalid or duplicate object key at byte %d", d.InputOffset())
				}
				seen[key] = true
			}
			child := pointer + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
			if v == '{' && key == "Type" {
				value, e := d.Token()
				if e != nil {
					return fmt.Errorf("invalid Type")
				}
				s, ok := value.(string)
				if !ok || s == "" {
					return fmt.Errorf("declared Type must be a nonempty string")
				}
				(*nodes)[idx].DeclaredType = s
				*nodes = append(*nodes, node{Pointer: child, Kind: "string", Status: "inventoried-unverified"})
			} else if e := readNode(d, child, depth+1, nodes); e != nil {
				return e
			}
		}
		end, e := d.Token()
		if e != nil || (v == '{' && end != json.Delim('}')) || (v == '[' && end != json.Delim(']')) {
			return fmt.Errorf("invalid closing delimiter")
		}
		return nil
	case string:
		n.Kind = "string"
	case json.Number:
		n.Kind = "number"
	case bool:
		n.Kind = "boolean"
	case nil:
		n.Kind = "null"
	default:
		return fmt.Errorf("unsupported JSON token")
	}
	*nodes = append(*nodes, n)
	return nil
}

func inventory(name string, raw []byte) (document, error) {
	result := document{File: name, SHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Nodes: []node{}}
	if len(raw) > maxBytes {
		return result, fmt.Errorf("document exceeds 16 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := readNode(d, "", 0, &result.Nodes); err != nil {
		return result, err
	}
	if _, err := d.Token(); err != io.EOF {
		return result, fmt.Errorf("trailing JSON content")
	}
	if result.Nodes[0].Kind != "object" && result.Nodes[0].Kind != "array" {
		return result, fmt.Errorf("root must be object or array")
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].Pointer < result.Nodes[j].Pointer })
	return result, nil
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("inventoryaapi", flag.ContinueOnError)
	input := flags.String("in", "", "JSON file or directory (including .folder.json)")
	version := flags.String("source-version", "", "declared source version; not a compatibility claim")
	plan := flags.Bool("plan", false, "include a non-executable conversion review plan (9.0.22.100 only)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *input == "" || strings.TrimSpace(*version) == "" || flags.NArg() != 0 {
		return fmt.Errorf("-in and -source-version are required; no positional arguments")
	}
	if *plan && *version != "9.0.22.100" {
		return fmt.Errorf("conversion review requires -source-version 9.0.22.100; other versions support inventory only")
	}
	root, err := filepath.Abs(*input)
	if err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	base := filepath.Dir(root)
	if info.IsDir() {
		base = root
	}
	files := []string{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not accepted")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("nonregular input")
		}
		if strings.EqualFold(filepath.Ext(path), ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no JSON documents found")
	}
	sort.Strings(files)
	r := report{Format: "regente-aapi-inventory/v1", SourceVersion: *version, Documents: []document{}}
	for _, path := range files {
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		raw, e := io.ReadAll(io.LimitReader(f, maxBytes+1))
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		name, e := filepath.Rel(base, path)
		if e != nil {
			return e
		}
		doc, e := inventory(filepath.ToSlash(name), raw)
		if e != nil {
			return fmt.Errorf("document %d: %w", len(r.Documents)+1, e)
		}
		if *plan {
			doc.Plan = reviewPlan(raw, doc, info.IsDir())
		}
		r.Documents = append(r.Documents, doc)
	}
	// Buffer until every document has passed validation: never emit partial success.
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = out.Write(append(b, '\n'))
	return err
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "inventoryaapi:", err)
		os.Exit(1)
	}
}
