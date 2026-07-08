// regente promote — D-9 Multi-environment promotion com Git flow NATIVO.
//
// Ambientes são BRANCHES do repo de workspace (ex.: dev → staging → main).
// Promover = fazer o estado dos paths promovíveis do branch origem virar o
// estado do branch destino — um commit normal, revisável, revertível:
//
//	regente promote -repo https://github.com/org/regente-workspace.git \
//	    -from dev -to staging                 # workspace inteiro
//	regente promote -from dev -to main -folders financeiro,pix   # parcial
//	regente promote ... -dry-run              # só mostra o diff, não commita
//
// Paths promovíveis: definitions/ (ou definitions/<folder> na promoção
// parcial), calendars/ e policies.yaml — código E política viajam juntos.
// Sem merge textual: o snapshot da origem SUBSTITUI o destino nos paths
// (ambientes não divergem "um pouco"; divergência é drift, e promoção zera).
// O servidor do ambiente destino pega a mudança pelo fluxo GitOps normal
// (webhook/poll) — o promote não fala com servidor nenhum.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func cmdPromote(args []string) error {
	fs := flag.NewFlagSet("promote", flag.ExitOnError)
	repoURL := fs.String("repo", envOr("REGENTE_GIT_SOURCE", ""), "repo do workspace (URL https ou caminho local)")
	from := fs.String("from", "", "branch de ORIGEM (ex.: dev)")
	to := fs.String("to", "", "branch de DESTINO (ex.: staging, main)")
	folders := fs.String("folders", "", "promoção parcial: folders separadas por vírgula (vazio = workspace inteiro)")
	message := fs.String("message", "", "mensagem do commit (default gerada)")
	token := fs.String("token", os.Getenv("GITHUB_TOKEN"), "PAT p/ https (env GITHUB_TOKEN)")
	dryRun := fs.Bool("dry-run", false, "mostra o que mudaria, sem commit/push")
	_ = fs.Parse(reorderArgs(args, "dry-run"))

	if *repoURL == "" || *from == "" || *to == "" {
		return errors.New("uso: regente promote -repo <url|path> -from <branch> -to <branch> [-folders a,b] [-dry-run]")
	}
	if *from == *to {
		return errors.New("-from e -to são o mesmo branch")
	}

	var auth transport.AuthMethod
	if *token != "" && strings.HasPrefix(*repoURL, "http") {
		auth = &githttp.BasicAuth{Username: "x-access-token", Password: *token}
	}

	// clone efêmero do branch DESTINO (o promote nunca toca clones de trabalho).
	tmp, err := os.MkdirTemp("", "regente-promote-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	repo, err := git.PlainClone(tmp, false, &git.CloneOptions{
		URL: *repoURL, Auth: auth,
		ReferenceName: plumbing.NewBranchReferenceName(*to),
		SingleBranch:  true,
	})
	if err != nil {
		return fmt.Errorf("clone %s@%s: %w", *repoURL, *to, err)
	}
	if err := repo.Fetch(&git.FetchOptions{
		Auth:     auth,
		RefSpecs: []config.RefSpec{config.RefSpec("+refs/heads/" + *from + ":refs/remotes/origin/" + *from)},
	}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("fetch %s: %w", *from, err)
	}
	fromRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", *from), true)
	if err != nil {
		return fmt.Errorf("branch de origem %q não existe no remoto", *from)
	}
	fromCommit, err := repo.CommitObject(fromRef.Hash())
	if err != nil {
		return err
	}
	fromTree, err := fromCommit.Tree()
	if err != nil {
		return err
	}

	prefixes := promotePaths(splitCSV(*folders))

	// 1. estado desejado: todos os arquivos da ORIGEM sob os prefixos.
	desired := map[string]*object.File{}
	if err := fromTree.Files().ForEach(func(f *object.File) error {
		if underAny(f.Name, prefixes) {
			desired[f.Name] = f
		}
		return nil
	}); err != nil {
		return err
	}
	if len(desired) == 0 {
		return fmt.Errorf("nada promovível em %s sob %v (folders erradas?)", *from, prefixes)
	}

	// 2. substitui os paths no worktree destino: apaga o que a origem não tem,
	//    escreve o conteúdo da origem (snapshot, não merge).
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	_ = filepath.WalkDir(tmp, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(path, tmp), string(os.PathSeparator)))
		if strings.HasPrefix(rel, ".git/") || !underAny(rel, prefixes) {
			return nil
		}
		if _, keep := desired[rel]; !keep {
			return os.Remove(path)
		}
		return nil
	})
	for name, f := range desired {
		dst := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		r, err := f.Reader()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return err
	}

	// 3. diff staged = o que a promoção muda. Vazio = ambientes já iguais.
	status, err := wt.Status()
	if err != nil {
		return err
	}
	var changes []string
	for file, st := range status {
		if st.Staging != git.Unmodified {
			changes = append(changes, fmt.Sprintf("%c %s", st.Staging, file))
		}
	}
	sort.Strings(changes)
	scope := "workspace inteiro"
	if *folders != "" {
		scope = "folders " + *folders
	}
	fmt.Printf("promote %s → %s (%s): %d mudança(s)\n", *from, *to, scope, len(changes))
	for _, c := range changes {
		fmt.Println("  " + c)
	}
	if len(changes) == 0 {
		fmt.Println("nada a promover — ambientes já idênticos nos paths.")
		return nil
	}
	if *dryRun {
		fmt.Println("dry-run: nenhum commit/push feito.")
		return nil
	}

	msg := *message
	if msg == "" {
		msg = fmt.Sprintf("promote(%s→%s): %s [de %s]", *from, *to, scope, fromRef.Hash().String()[:7])
	}
	sha, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "regente-promote", Email: "regente@local", When: time.Now()},
	})
	if err != nil {
		return err
	}
	if err := repo.Push(&git.PushOptions{Auth: auth}); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	fmt.Printf("promovido: commit %s em %s\n", sha.String()[:7], *to)
	return nil
}

// promotePaths — o que viaja numa promoção. Política e calendars fazem parte
// do ambiente: promover jobs sem a política que os valida seria meio-ambiente.
func promotePaths(folders []string) []string {
	if len(folders) == 0 {
		return []string{"definitions/", "calendars/", "policies.yaml"}
	}
	out := make([]string, 0, len(folders)+2)
	for _, f := range folders {
		out = append(out, "definitions/"+f+"/")
	}
	return append(out, "calendars/", "policies.yaml")
}

func underAny(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if path == strings.TrimSuffix(p, "/") || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
