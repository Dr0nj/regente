package storage

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const maxDraftBytes = 64 << 20
const maxDraftCompressed = 16 << 20

type draftObject struct {
	Hash string
	Type plumbing.ObjectType
	Data []byte
}
type draftFile struct {
	Name string
	Mode uint32
	Data []byte
}
type draftSnapshot struct {
	Undo       json.RawMessage
	Format     int
	Source     string
	Branch     string
	BaseSHA    string
	HeadSHA    string
	HeadBranch string
	Shallow    []plumbing.Hash
	Objects    []draftObject
	Files      []draftFile
	Folders    []string
	NewFolders []string
}

// Apenas caminhos portáteis relativos; nunca permitir .git, links ou traversal.
func safeDraftPath(name string) bool {
	if name == "" || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		stem := strings.ToUpper(strings.Split(part, ".")[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
			return false
		}
		if part == "." || part == ".." || strings.EqualFold(part, ".git") || strings.TrimRight(part, ". ") != part {
			return false
		}
	}
	return true
}

func captureDraft(s *DesignSession) (string, string, error) {
	repo, err := git.PlainOpen(s.Path)
	if err != nil {
		return "", "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", "", err
	}
	snap := draftSnapshot{Format: 1, Source: s.Git.source, Branch: s.Git.branch, BaseSHA: s.BaseSHA, HeadSHA: head.Hash().String(), HeadBranch: head.Name().String(), Folders: s.Folders, NewFolders: s.NewFolders}
	snap.Undo = s.Undo
	snap.Shallow, err = repo.Storer.Shallow()
	if err != nil {
		return "", "", err
	}
	iter, err := repo.Storer.IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		return "", "", err
	}
	defer iter.Close()
	size := 0
	err = iter.ForEach(func(o plumbing.EncodedObject) error {
		if o.Size() > maxDraftBytes-int64(size) {
			return fmt.Errorf("draft exceeds 64 MiB expanded limit")
		}
		reader, e := o.Reader()
		if e != nil {
			return e
		}
		data, e := io.ReadAll(io.LimitReader(reader, maxDraftBytes+1))
		closeErr := reader.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		size += len(data)
		if size > maxDraftBytes {
			return fmt.Errorf("draft exceeds 64 MiB expanded limit")
		}
		snap.Objects = append(snap.Objects, draftObject{o.Hash().String(), o.Type(), data})
		return nil
	})
	if err != nil {
		return "", "", err
	}
	sort.Slice(snap.Objects, func(i, j int) bool { return snap.Objects[i].Hash < snap.Objects[j].Hash })
	err = filepath.WalkDir(s.Path, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == s.Path {
			return nil
		}
		rel, e := filepath.Rel(s.Path, p)
		if e != nil {
			return e
		}
		name := filepath.ToSlash(rel)
		if name == ".git" {
			return filepath.SkipDir
		}
		if !safeDraftPath(name) {
			return fmt.Errorf("draft contains unsafe path %q", name)
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("draft contains unsupported file %q; preserve original clone", name)
		}
		if info.Size() > int64(maxDraftBytes-size) {
			return fmt.Errorf("draft exceeds 64 MiB expanded limit")
		}
		data, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		size += len(data)
		snap.Files = append(snap.Files, draftFile{name, uint32(info.Mode().Perm()), data})
		return nil
	})
	if err != nil {
		return "", "", err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return "", "", err
	}
	if len(raw) > maxDraftBytes {
		return "", "", fmt.Errorf("draft exceeds 64 MiB expanded limit")
	}
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err = zw.Write(raw); err != nil {
		return "", "", err
	}
	if err = zw.Close(); err != nil {
		return "", "", err
	}
	if b.Len() > maxDraftCompressed {
		return "", "", fmt.Errorf("draft exceeds 16 MiB compressed limit")
	}
	return base64.StdEncoding.EncodeToString(b.Bytes()), fmt.Sprintf("%x", sha256.Sum256(b.Bytes())), nil
}

func decodeDraft(payload, checksum string) (*draftSnapshot, error) {
	if len(payload) > base64.StdEncoding.EncodedLen(maxDraftCompressed) {
		return nil, fmt.Errorf("draft exceeds size limit")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != checksum {
		return nil, fmt.Errorf("draft checksum mismatch")
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	raw, err := io.ReadAll(io.LimitReader(zr, maxDraftBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxDraftBytes {
		return nil, fmt.Errorf("draft exceeds expanded limit")
	}
	var snap draftSnapshot
	if err = json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	if snap.Format != 1 || len(snap.BaseSHA) != 40 || len(snap.HeadSHA) != 40 || !strings.HasPrefix(snap.HeadBranch, "refs/heads/") {
		return nil, fmt.Errorf("invalid draft format/base/head")
	}
	if err := plumbing.ReferenceName(snap.HeadBranch).Validate(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range snap.Files {
		if !safeDraftPath(f.Name) || seen[f.Name] || f.Mode & ^uint32(0777) != 0 {
			return nil, fmt.Errorf("invalid draft file %q", f.Name)
		}
		seen[f.Name] = true
	}
	for _, folder := range append(append([]string{}, snap.Folders...), snap.NewFolders...) {
		if !safeDraftPath(folder) {
			return nil, fmt.Errorf("invalid draft folder")
		}
	}
	return &snap, nil
}

// Cache privado reconstruível sem rede e sem copiar config/credenciais do clone.
func restoreDraft(dir string, snap *draftSnapshot) error {
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		return err
	}
	for _, o := range snap.Objects {
		if o.Type != plumbing.CommitObject && o.Type != plumbing.TreeObject && o.Type != plumbing.BlobObject && o.Type != plumbing.TagObject {
			return fmt.Errorf("unsupported draft object")
		}
		if len(o.Hash) != 40 || plumbing.ComputeHash(o.Type, o.Data).String() != o.Hash {
			return fmt.Errorf("draft Git object checksum mismatch")
		}
		encoded := repo.Storer.NewEncodedObject()
		encoded.SetType(o.Type)
		encoded.SetSize(int64(len(o.Data)))
		wr, e := encoded.Writer()
		if e != nil {
			return e
		}
		_, e = wr.Write(o.Data)
		ce := wr.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if _, e = repo.Storer.SetEncodedObject(encoded); e != nil {
			return e
		}
	}
	// Verifica a árvore que será materializada antes de qualquer checkout.
	for _, sha := range []string{snap.BaseSHA, snap.HeadSHA} {
		commit, e := repo.CommitObject(plumbing.NewHash(sha))
		if e != nil {
			return fmt.Errorf("draft missing Git base/head: %w", e)
		}
		tree, e := commit.Tree()
		if e != nil {
			return e
		}
		e = tree.Files().ForEach(func(f *object.File) error {
			if !safeDraftPath(f.Name) || f.Mode == filemode.Symlink || f.Mode == filemode.Submodule {
				return fmt.Errorf("unsupported Git file %q", f.Name)
			}
			return nil
		})
		if e != nil {
			return e
		}
	}
	if err = repo.Storer.SetShallow(snap.Shallow); err != nil {
		return err
	}
	branch := plumbing.ReferenceName(snap.HeadBranch)
	if err = repo.Storer.SetReference(plumbing.NewHashReference(branch, plumbing.NewHash(snap.HeadSHA))); err != nil {
		return err
	}
	if err = repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branch)); err != nil {
		return err
	}
	if _, err = repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{snap.Source}}); err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err = wt.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: plumbing.NewHash(snap.HeadSHA)}); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != ".git" {
			if err = os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	for _, f := range snap.Files {
		p := filepath.Join(dir, filepath.FromSlash(f.Name))
		if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(p, f.Data, os.FileMode(f.Mode)); err != nil {
			return err
		}
	}
	return nil
}
