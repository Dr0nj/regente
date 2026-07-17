package api

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// urlName devolve o parâmetro {param} da rota JÁ decodificado. O chi entrega o
// segmento AINDA percent-encoded quando o cliente escapou caracteres especiais:
// o front manda nomes de recurso via encodeURIComponent, então um nome como
// "FOO@Odate" chega na rota como "FOO%40Odate" (e um espaço como "%20"). Sem
// decodificar, o name não bate com o gravado e o handler (set/unset/delete/
// rename/…) vira NO-OP silencioso — a rota casa (HTTP 2xx, o toast diz "ok"),
// mas 0 linhas/arquivos saem. Era por isso que condições com "@" no nome não
// podiam ser deletadas (963b8b9); o mesmo latente valia para QUALQUER recurso
// endereçado por nome na rota (calendars, resources, variables, folders,
// templates). PathUnescape é seguro/idempotente: um nome sem "%" volta igual, e
// numa sequência "%" inválida cai no valor cru (mesmo comportamento de antes).
func urlName(r *http.Request, param string) string {
	raw := chi.URLParam(r, param)
	if dec, err := url.PathUnescape(raw); err == nil {
		return dec
	}
	return raw
}
