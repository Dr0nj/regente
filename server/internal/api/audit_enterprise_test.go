package api

// E2 — auditoria enterprise: PUT /api/settings audita mudanças SEM vazar
// segredo; GET /api/audit/export pagina com cursor after_id estável.

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Dr0nj/regente-server/internal/db"
)

func auditEventCount(t *testing.T, d *db.DB, kind string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE kind=?`, kind).Scan(&n); err != nil {
		t.Fatalf("count audit_events: %v", err)
	}
	return n
}

func TestSettingsAudit_MudancaGeraEventoSemVazarSegredo(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()

	resp := doReq(t, c, http.MethodPut, srv.URL+"/api/settings", "test-token",
		`{"daily_at":"01:30","alert_smtp_password":"s3cr3t-pw"}`)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PUT settings esperava 200, veio %d", resp.StatusCode)
	}
	if n := auditEventCount(t, d, "settings.write"); n != 1 {
		t.Fatalf("esperava 1 evento settings.write, veio %d", n)
	}
	var detail, actor string
	if err := d.QueryRow(`SELECT detail, actor FROM audit_events WHERE kind='settings.write'`).Scan(&detail, &actor); err != nil {
		t.Fatalf("ler evento: %v", err)
	}
	if !strings.Contains(detail, `daily_at: "" → "01:30"`) {
		t.Fatalf("detail deveria ter o de→para de daily_at, veio %q", detail)
	}
	if !strings.Contains(detail, "alert_smtp_password: (alterado)") {
		t.Fatalf("detail deveria registrar a mudança do segredo mascarada, veio %q", detail)
	}
	if strings.Contains(detail, "s3cr3t") {
		t.Fatalf("VAZAMENTO: o valor do segredo apareceu na auditoria: %q", detail)
	}

	// Re-enviar os MESMOS valores é no-op → não audita de novo.
	resp = doReq(t, c, http.MethodPut, srv.URL+"/api/settings", "test-token",
		`{"daily_at":"01:30","alert_smtp_password":"s3cr3t-pw"}`)
	resp.Body.Close()
	if n := auditEventCount(t, d, "settings.write"); n != 1 {
		t.Fatalf("PUT no-op não deveria gerar evento novo; veio %d", n)
	}

	// Mudança real → segundo evento.
	resp = doReq(t, c, http.MethodPut, srv.URL+"/api/settings", "test-token", `{"daily_at":"02:00"}`)
	resp.Body.Close()
	if n := auditEventCount(t, d, "settings.write"); n != 2 {
		t.Fatalf("mudança real deveria gerar 2º evento; veio %d", n)
	}

	// A UI devolve o objeto do GET inteiro no PUT: flag sintética "*_set" e
	// chave ausente re-enviada como "" NÃO viram linha nem evento.
	resp = doReq(t, c, http.MethodPut, srv.URL+"/api/settings", "test-token",
		`{"alert_smtp_password_set":"true","env_label":""}`)
	resp.Body.Close()
	if n := auditEventCount(t, d, "settings.write"); n != 2 {
		t.Fatalf("flag _set/no-op de chave ausente não deveria auditar; veio %d eventos", n)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM settings WHERE key='alert_smtp_password_set'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("flag sintética _set não pode virar linha na tabela settings (n=%d err=%v)", n, err)
	}
}

// exportLines faz o GET e devolve as linhas JSONL decodificadas.
func exportLines(t *testing.T, srv string, c *http.Client, token, query string) []exportLine {
	t.Helper()
	resp := doReq(t, c, http.MethodGet, srv+"/api/audit/export"+query, token, "")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("export %s esperava 200, veio %d", query, resp.StatusCode)
	}
	var out []exportLine
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var l exportLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("linha JSONL inválida %q: %v", sc.Text(), err)
		}
		out = append(out, l)
	}
	return out
}

func TestAuditExport_PaginaComCursorEstavel(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()

	for i, kind := range []string{"ordered", "started", "finished"} {
		if _, err := d.Exec(`INSERT INTO instance_events(instance_id, kind, actor, message) VALUES(?,?,?,?)`,
			"inst-1", kind, "scheduler", "m"); err != nil {
			t.Fatalf("seed instance_events %d: %v", i, err)
		}
	}
	for _, kind := range []string{"auth.login", "definition.write"} {
		if _, err := d.Exec(`INSERT INTO audit_events(kind, actor, action, outcome) VALUES(?,?,?,?)`,
			kind, "admin", "x", "success"); err != nil {
			t.Fatalf("seed audit_events: %v", err)
		}
	}

	// Página 1: só instance_events (fonte 1 em ordem de id).
	p1 := exportLines(t, srv.URL, c, "test-token", "?limit=2")
	if len(p1) != 2 || p1[0].Cursor != "i:1" || p1[1].Cursor != "i:2" || p1[0].Source != "instance" {
		t.Fatalf("página 1 esperava [i:1 i:2], veio %+v", p1)
	}
	// Página 2 retoma do cursor: fecha a fonte instance e entra na audit.
	p2 := exportLines(t, srv.URL, c, "test-token", "?limit=2&after_id="+p1[1].Cursor)
	if len(p2) != 2 || p2[0].Cursor != "i:3" || p2[1].Cursor != "a:1" || p2[1].Source != "audit" {
		t.Fatalf("página 2 esperava [i:3 a:1], veio %+v", p2)
	}
	// Página 3: resto da audit; página 4: vazia (fim).
	p3 := exportLines(t, srv.URL, c, "test-token", "?limit=2&after_id="+p2[1].Cursor)
	if len(p3) != 1 || p3[0].Cursor != "a:2" || p3[0].Kind != "definition.write" {
		t.Fatalf("página 3 esperava [a:2], veio %+v", p3)
	}
	if p4 := exportLines(t, srv.URL, c, "test-token", "?limit=2&after_id=a:2"); len(p4) != 0 {
		t.Fatalf("depois do fim deveria vir vazio, veio %+v", p4)
	}

	// Filtro temporal: janela no futuro não devolve nada.
	if got := exportLines(t, srv.URL, c, "test-token", "?from=2099-01-01"); len(got) != 0 {
		t.Fatalf("from futuro deveria filtrar tudo, veio %d linhas", len(got))
	}
	if got := exportLines(t, srv.URL, c, "test-token", "?to=2000-01-01"); len(got) != 0 {
		t.Fatalf("to no passado deveria filtrar tudo, veio %d linhas", len(got))
	}

	// format desconhecido → 400.
	resp := doReq(t, c, http.MethodGet, srv.URL+"/api/audit/export?format=csv", "test-token", "")
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("format=csv esperava 400, veio %d", resp.StatusCode)
	}
}

func TestAuditExport_AdminOnly(t *testing.T) {
	srv, d := newOpsTestServer(t)
	c := srv.Client()
	op := newOperatorToken(t, d, "op", nil)
	resp := doReq(t, c, http.MethodGet, srv.URL+"/api/audit/export", op, "")
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("operator no export esperava 403, veio %d", resp.StatusCode)
	}
}
