package serveragent

import (
	"github.com/Dr0nj/regente-server/internal/hub"
	"testing"
)

func TestProductionScope(t *testing.T) {
	h := hub.New()
	c := StartScoped(h, nil, "prod", nil)
	defer h.Unregister(c)
	if !h.HasAgent(ID, "HTTP", "prod") {
		t.Fatal("HTTP produtivo indisponível")
	}
	for _, env := range []string{"", "staging"} {
		if h.HasAgent(ID, "HTTP", env) {
			t.Fatal("SERVER-AGENT aceitou outro escopo")
		}
	}
}
