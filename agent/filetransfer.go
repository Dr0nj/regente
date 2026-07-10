// FILE_TRANSFER (MFT) — paridade Control-M Managed File Transfer: move/copia
// arquivos entre LOCAL, SFTP e S3 pelo agente, com verificação e pós-ações.
// Mesmo espírito dos demais executores: pure-Go (pkg/sftp + x/crypto), S3 via
// SigV4 da stdlib (reusa os primitivos do aws.go, sem aws-sdk).
//
// Endpoints (em src E dst, combináveis à vontade — local→sftp, s3→local,
// sftp→sftp entre hosts diferentes…):
//
//	caminho local                     /data/out/f.csv  ·  C:\saida\f.csv
//	sftp://user:pass@host:porta/caminho   (porta default 22; senha na URL,
//	                                       no param password ou keyPath)
//	s3://bucket/chave                     (creds via params ou envs AWS_*)
//
// Params:
//
//	src, dst      (obrigatórios) — origem/destino; glob (*.csv) na ORIGEM
//	              local/sftp; múltiplos arquivos exigem destino diretório
//	              (termine com /); S3 = chave exata (sem glob)
//	checksum      true = relê o DESTINO e compara SHA-256 (fim-a-fim)
//	deleteSource  true = remove a origem após transferir (+verificar) — move
//	overwrite     false = falha se o destino já existe (default true)
//	mkdirs        cria diretórios de destino que faltam (default true)
//	keyPath       chave privada p/ endpoints sftp (arquivo no host do agente)
//	password      senha sftp (a userinfo da URL vence)
//	hostKeyFingerprint  pin "SHA256:..." do host sftp; vazio = aceita qualquer
//	region, accessKeyId, secretAccessKey, sessionToken  — S3 (default envs AWS_*)
//	s3Endpoint    override da URL base do S3 (testes/MinIO; path-style)
//
// Escrita ATÔMICA onde o backend permite: grava em <dst>.part e renomeia no
// fim — um FILE_WATCH com stableSec do outro lado nunca vê arquivo parcial.
// PUT no S3 já é atômico por natureza.
//
// Exit code: 0 = tudo transferido; 1 = falha de transferência/verificação;
// -1 = params inválidos ou endpoint inacessível.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func runFileTransfer(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	src, _ := params["src"].(string)
	dst, _ := params["dst"].(string)
	if src == "" || dst == "" {
		return -1, "missing 'src' or 'dst' param"
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)

	srcFS, srcPath, err := openEndpoint(src, params, deadline)
	if err != nil {
		return -1, "src: " + err.Error()
	}
	defer srcFS.close()
	dstFS, dstPath, err := openEndpoint(dst, params, deadline)
	if err != nil {
		return -1, "dst: " + err.Error()
	}
	defer dstFS.close()

	files, err := srcFS.expand(srcPath)
	if err != nil {
		return -1, "src: " + err.Error()
	}
	if len(files) == 0 {
		return 1, fmt.Sprintf("origem sem arquivos: %s", src)
	}
	dstIsDir := wantsDir(dstFS, dstPath)
	if len(files) > 1 && !dstIsDir {
		return 1, fmt.Sprintf("origem casou %d arquivos mas o destino não é diretório/prefixo (termine com /): %s", len(files), dst)
	}

	checksum := boolParam(params, "checksum", false)
	del := boolParam(params, "deleteSource", false)
	overwrite := boolParam(params, "overwrite", true)
	mkdirs := boolParam(params, "mkdirs", true)

	emit(fmt.Sprintf("[mft] %s → %s (%d arquivo(s), checksum=%v, deleteSource=%v)\n",
		src, dst, len(files), checksum, del))

	var report strings.Builder
	var totalBytes int64
	for _, f := range files {
		target := dstPath
		if dstIsDir {
			target = dstFS.join(dstPath, srcFS.base(f))
		}
		if !overwrite {
			ok, err := dstFS.exists(target)
			if err != nil {
				return 1, report.String() + "destino: " + err.Error()
			}
			if ok {
				return 1, report.String() + fmt.Sprintf("destino já existe: %s (overwrite=false)", target)
			}
		}
		if mkdirs {
			if d := dstFS.dir(target); d != "" && d != "." && d != "/" {
				_ = dstFS.mkdirAll(d) // best-effort; erro real aparece na escrita
			}
		}
		n, sum, err := copyOneFile(srcFS, f, dstFS, target, deadline)
		if err != nil {
			return 1, report.String() + fmt.Sprintf("falha em %s → %s: %s", f, target, err.Error())
		}
		totalBytes += n
		line := fmt.Sprintf("  %s → %s (%s)", f, target, humanBytes(n))
		if checksum {
			ok, got, err := verifyChecksum(dstFS, target, sum)
			if err != nil {
				return 1, report.String() + line + "\nverificação: " + err.Error()
			}
			if !ok {
				return 1, report.String() + line + fmt.Sprintf("\nchecksum DIVERGENTE em %s: origem %s… ≠ destino %s…", target, sum[:12], got[:12])
			}
			line += " sha256✓"
		}
		if del {
			if err := srcFS.remove(f); err != nil {
				return 1, report.String() + line + "\ntransferido, mas falhou ao remover a origem: " + err.Error()
			}
			line += " origem removida"
		}
		emit(line + "\n")
		report.WriteString(line + "\n")
	}
	return 0, fmt.Sprintf("[mft] OK: %d arquivo(s), %s\n%s", len(files), humanBytes(totalBytes), report.String())
}

// copyOneFile transfere um arquivo hasheando a origem em trânsito (SHA-256
// pro relatório/verificação) e respeitando o deadline do job a cada Read.
func copyOneFile(srcFS xferFS, from string, dstFS xferFS, to string, deadline time.Time) (int64, string, error) {
	r, size, err := srcFS.open(from)
	if err != nil {
		return 0, "", err
	}
	defer r.Close()
	h := sha256.New()
	dr := &deadlineReader{r: io.TeeReader(r, h), deadline: deadline}
	if err := dstFS.writeFile(to, dr, size); err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}

func verifyChecksum(fs xferFS, p, want string) (bool, string, error) {
	r, _, err := fs.open(p)
	if err != nil {
		return false, "", err
	}
	defer r.Close()
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return false, "", err
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == want, got, nil
}

// deadlineReader corta a transferência quando o timeout do job estoura —
// sem goroutine órfã copiando um arquivo gigante depois do NOTOK.
type deadlineReader struct {
	r        io.Reader
	deadline time.Time
}

func (d *deadlineReader) Read(p []byte) (int, error) {
	if time.Now().After(d.deadline) {
		return 0, fmt.Errorf("timeout do job durante a transferência")
	}
	return d.r.Read(p)
}

// wantsDir — o destino é diretório/prefixo? Suffix explícito vale mesmo que
// ainda não exista (mkdirs cria); senão pergunta ao backend.
func wantsDir(fs xferFS, p string) bool {
	if strings.HasSuffix(p, "/") || strings.HasSuffix(p, "\\") {
		return true
	}
	return fs.isDir(p)
}

// boolParam lê um bool de params aceitando bool nativo e "true"/"false"
// string (o YAML/JSON dos dois jeitos que o schema KindBool aceita).
func boolParam(params map[string]interface{}, key string, def bool) bool {
	switch v := params[key].(type) {
	case bool:
		return v
	case string:
		if strings.EqualFold(v, "true") {
			return true
		}
		if strings.EqualFold(v, "false") {
			return false
		}
	}
	return def
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

/* ── endpoints ─────────────────────────────────────────────────────────── */

// xferFS — o mínimo que cada backend (local/sftp/s3) precisa saber fazer.
type xferFS interface {
	kind() string
	// expand resolve glob na origem (local/sftp); padrão sem meta = caminho exato.
	expand(pattern string) ([]string, error)
	open(p string) (io.ReadCloser, int64, error)
	// writeFile grava r em p — atômico (.part + rename) onde o backend permite.
	writeFile(p string, r io.Reader, size int64) error
	exists(p string) (bool, error)
	isDir(p string) bool
	remove(p string) error
	mkdirAll(dir string) error
	join(dir, name string) string
	base(p string) string
	dir(p string) string
	close()
}

// openEndpoint decide o backend pela forma do caminho. Cuidado clássico:
// url.Parse acha que "C:\x" tem scheme "c" — Windows path é SEMPRE local,
// por isso o match é por prefixo explícito sftp:// | s3:// | file://.
func openEndpoint(raw string, params map[string]interface{}, deadline time.Time) (xferFS, string, error) {
	low := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(low, "sftp://"):
		u, err := url.Parse(raw)
		if err != nil {
			return nil, "", err
		}
		if u.Path == "" {
			return nil, "", fmt.Errorf("sftp: URL sem caminho: %s", raw)
		}
		fs, err := dialSFTP(u, params, deadline)
		if err != nil {
			return nil, "", err
		}
		return fs, u.Path, nil
	case strings.HasPrefix(low, "s3://"):
		u, err := url.Parse(raw)
		if err != nil {
			return nil, "", err
		}
		fs, err := newS3FS(u, params, deadline)
		if err != nil {
			return nil, "", err
		}
		return fs, strings.TrimPrefix(u.Path, "/"), nil
	case strings.HasPrefix(low, "file://"):
		return localFS{}, raw[len("file://"):], nil
	default:
		return localFS{}, raw, nil
	}
}

/* ── local ─────────────────────────────────────────────────────────────── */

type localFS struct{}

func (localFS) kind() string { return "local" }

func (localFS) expand(pattern string) ([]string, error) {
	if !strings.ContainsAny(pattern, "*?[") {
		if _, err := os.Stat(pattern); err != nil {
			return nil, nil // origem inexistente = 0 arquivos (mensagem única no caller)
		}
		return []string{pattern}, nil
	}
	m, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(m)
	return m, nil
}

func (localFS) open(p string) (io.ReadCloser, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func (localFS) writeFile(p string, r io.Reader, _ int64) error {
	tmp := p + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Windows: rename não sobrescreve — remove antes (overwrite já foi gated).
	if _, err := os.Stat(p); err == nil {
		if err := os.Remove(p); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return os.Rename(tmp, p)
}

func (localFS) exists(p string) (bool, error) {
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (localFS) isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func (localFS) remove(p string) error        { return os.Remove(p) }
func (localFS) mkdirAll(dir string) error    { return os.MkdirAll(dir, 0o755) }
func (localFS) join(dir, name string) string { return filepath.Join(dir, name) }
func (localFS) base(p string) string         { return filepath.Base(p) }
func (localFS) dir(p string) string          { return filepath.Dir(p) }
func (localFS) close()                       {}

/* ── sftp ──────────────────────────────────────────────────────────────── */

type sftpFS struct {
	c    *sftp.Client
	conn *ssh.Client // nil quando o client veio de um pipe (testes)
}

func dialSFTP(u *url.URL, params map[string]interface{}, deadline time.Time) (*sftpFS, error) {
	user := u.User.Username()
	if user == "" {
		return nil, fmt.Errorf("sftp: URL sem usuário (sftp://user@host/...)")
	}
	pw, hasPW := u.User.Password()
	if !hasPW {
		pw, _ = params["password"].(string)
	}
	var auth []ssh.AuthMethod
	if kp, _ := params["keyPath"].(string); kp != "" {
		key, err := os.ReadFile(kp)
		if err != nil {
			return nil, fmt.Errorf("sftp keyPath: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("sftp keyPath: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if pw != "" {
		auth = append(auth, ssh.Password(pw))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("sftp: sem credencial (senha na URL, param password ou keyPath)")
	}
	hk := ssh.InsecureIgnoreHostKey()
	if fp, _ := params["hostKeyFingerprint"].(string); fp != "" {
		want := fp
		hk = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if got := ssh.FingerprintSHA256(key); got != want {
				return fmt.Errorf("host key de %s não bate: %s (esperado %s)", hostname, got, want)
			}
			return nil
		}
	}
	port := u.Port()
	if port == "" {
		port = "22"
	}
	dialTO := time.Until(deadline)
	if dialTO > 30*time.Second {
		dialTO = 30 * time.Second
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(u.Hostname(), port), &ssh.ClientConfig{
		User: user, Auth: auth, HostKeyCallback: hk, Timeout: dialTO,
	})
	if err != nil {
		return nil, fmt.Errorf("sftp dial %s: %w", u.Host, err)
	}
	c, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("sftp subsystem: %w", err)
	}
	return &sftpFS{c: c, conn: conn}, nil
}

func (s *sftpFS) kind() string { return "sftp" }

func (s *sftpFS) expand(pattern string) ([]string, error) {
	if !strings.ContainsAny(pattern, "*?[") {
		if _, err := s.c.Stat(pattern); err != nil {
			return nil, nil
		}
		return []string{pattern}, nil
	}
	m, err := s.c.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(m)
	return m, nil
}

func (s *sftpFS) open(p string) (io.ReadCloser, int64, error) {
	f, err := s.c.Open(p)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func (s *sftpFS) writeFile(p string, r io.Reader, _ int64) error {
	tmp := p + ".part"
	f, err := s.c.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		s.c.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		s.c.Remove(tmp)
		return err
	}
	_ = s.c.Remove(p) // rename clássico não sobrescreve (overwrite já foi gated)
	if err := s.c.PosixRename(tmp, p); err != nil {
		if err2 := s.c.Rename(tmp, p); err2 != nil {
			s.c.Remove(tmp)
			return err2
		}
	}
	return nil
}

func (s *sftpFS) exists(p string) (bool, error) {
	_, err := s.c.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *sftpFS) isDir(p string) bool {
	st, err := s.c.Stat(p)
	return err == nil && st.IsDir()
}

func (s *sftpFS) remove(p string) error        { return s.c.Remove(p) }
func (s *sftpFS) mkdirAll(dir string) error    { return s.c.MkdirAll(dir) }
func (s *sftpFS) join(dir, name string) string { return path.Join(dir, name) }
func (s *sftpFS) base(p string) string         { return path.Base(p) }
func (s *sftpFS) dir(p string) string          { return path.Dir(p) }

func (s *sftpFS) close() {
	s.c.Close()
	if s.conn != nil {
		s.conn.Close()
	}
}

/* ── s3 ────────────────────────────────────────────────────────────────── */

const s3UnsignedPayload = "UNSIGNED-PAYLOAD"

type s3FS struct {
	bucket  string
	region  string
	baseURL string // path-style: baseURL/bucket/chave
	ak, sk  string
	token   string
	client  *http.Client
}

func newS3FS(u *url.URL, params map[string]interface{}, deadline time.Time) (*s3FS, error) {
	if u.Host == "" {
		return nil, fmt.Errorf("s3: URL sem bucket (s3://bucket/chave)")
	}
	region := strFromParamsOrEnv(params, "region", "AWS_REGION")
	ak := strFromParamsOrEnv(params, "accessKeyId", "AWS_ACCESS_KEY_ID")
	sk := strFromParamsOrEnv(params, "secretAccessKey", "AWS_SECRET_ACCESS_KEY")
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("s3: missing credentials (accessKeyId/secretAccessKey ou envs AWS_*)")
	}
	base, _ := params["s3Endpoint"].(string)
	if base == "" {
		if region == "" {
			return nil, fmt.Errorf("s3: missing region (param ou AWS_REGION)")
		}
		base = "https://s3." + region + ".amazonaws.com"
	}
	if region == "" {
		region = "us-east-1" // endpoint custom sem região: assinatura precisa de UMA
	}
	return &s3FS{
		bucket: u.Host, region: region, baseURL: strings.TrimRight(base, "/"),
		ak: ak, sk: sk, token: strFromParamsOrEnv(params, "sessionToken", "AWS_SESSION_TOKEN"),
		client: &http.Client{Timeout: time.Until(deadline)},
	}, nil
}

// do assina (SigV4 c/ x-amz-content-sha256 — o S3 exige, diferente do Lambda)
// e executa uma operação de objeto.
func (s *s3FS) do(method, key string, body io.Reader, size int64, payloadHash string) (*http.Response, error) {
	urlStr := s.baseURL + (&url.URL{Path: "/" + s.bucket + "/" + key}).EscapedPath()
	hdrs, err := sigv4HeadersHash(method, urlStr, s.region, "s3", payloadHash, s.ak, s.sk, s.token, time.Now())
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, err
	}
	if size >= 0 {
		req.ContentLength = size
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	return s.client.Do(req)
}

func (s *s3FS) kind() string { return "s3" }

func (s *s3FS) expand(pattern string) ([]string, error) {
	if strings.ContainsAny(pattern, "*?[") {
		return nil, fmt.Errorf("glob não suportado em origem s3:// (use a chave exata)")
	}
	ok, err := s.exists(pattern)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return []string{pattern}, nil
}

func (s *s3FS) open(p string) (io.ReadCloser, int64, error) {
	resp, err := s.do(http.MethodGet, p, nil, -1, sha256hex(nil))
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, 0, fmt.Errorf("s3 GET %s: HTTP %d %s", p, resp.StatusCode, trunc(b, 200))
	}
	return resp.Body, resp.ContentLength, nil
}

func (s *s3FS) writeFile(p string, r io.Reader, size int64) error {
	// PUT único com payload não-assinado (streaming — sem carregar o arquivo
	// inteiro em memória pra hashear); PUT de objeto já é atômico no S3.
	resp, err := s.do(http.MethodPut, p, io.NopCloser(r), size, s3UnsignedPayload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3 PUT %s: HTTP %d %s", p, resp.StatusCode, trunc(b, 200))
	}
	return nil
}

func (s *s3FS) exists(p string) (bool, error) {
	resp, err := s.do(http.MethodHead, p, nil, -1, sha256hex(nil))
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("s3 HEAD %s: HTTP %d", p, resp.StatusCode)
}

// isDir — no S3 "diretório" é prefixo: chave vazia (raiz do bucket) ou com /
// final. O suffix já é aceito por wantsDir; aqui só cobre a raiz.
func (s *s3FS) isDir(p string) bool { return p == "" }

func (s *s3FS) remove(p string) error {
	resp, err := s.do(http.MethodDelete, p, nil, -1, sha256hex(nil))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("s3 DELETE %s: HTTP %d", p, resp.StatusCode)
	}
	return nil
}

func (s *s3FS) mkdirAll(string) error { return nil } // prefixo não precisa existir

func (s *s3FS) join(dir, name string) string {
	if dir == "" {
		return name
	}
	return strings.TrimSuffix(dir, "/") + "/" + name
}

func (s *s3FS) base(p string) string { return path.Base(p) }
func (s *s3FS) dir(p string) string  { return "" } // mkdirAll é no-op; nada a criar
func (s *s3FS) close()               {}

// sigv4HeadersHash — variante do sigv4Headers (aws.go) pra S3: recebe o HASH
// do payload pronto (streaming/UNSIGNED-PAYLOAD) e assina TAMBÉM o header
// x-amz-content-sha256, que o S3 exige e o Lambda dispensa.
func sigv4HeadersHash(method, rawURL, region, service, payloadHash, ak, sk, token string, now time.Time) (map[string]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	t := now.UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")

	signed := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	vals := map[string]string{"host": u.Host, "x-amz-content-sha256": payloadHash, "x-amz-date": amzDate}
	if token != "" {
		signed = append(signed, "x-amz-security-token")
		vals["x-amz-security-token"] = token
	}
	sort.Strings(signed)
	var ch strings.Builder
	for _, h := range signed {
		ch.WriteString(h + ":" + vals[h] + "\n")
	}
	signedHeaders := strings.Join(signed, ";")

	uri := u.EscapedPath()
	if uri == "" {
		uri = "/"
	}
	canonicalReq := method + "\n" + uri + "\n" + u.RawQuery + "\n" + ch.String() + "\n" + signedHeaders + "\n" + payloadHash
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256hex([]byte(canonicalReq))

	kDate := hmacSHA256([]byte("AWS4"+sk), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	auth := "AWS4-HMAC-SHA256 Credential=" + ak + "/" + scope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	h := map[string]string{"Authorization": auth, "X-Amz-Date": amzDate, "X-Amz-Content-Sha256": payloadHash}
	if token != "" {
		h["X-Amz-Security-Token"] = token
	}
	return h, nil
}
