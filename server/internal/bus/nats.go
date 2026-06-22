// Transporte NATS real (R5). Mantido fino e isolado: o resto do pacote depende só
// da interface Transport, então a lógica de fan-out/roteamento/presença é testada
// com um transporte fake, sem subir um NATS.
package bus

import (
	"time"

	"github.com/nats-io/nats.go"
)

type natsTransport struct{ nc *nats.Conn }

// DialNATS conecta ao NATS e devolve um Transport. Reconecta indefinidamente
// (NAT-friendly e resiliente a quedas do broker).
func DialNATS(url string) (Transport, error) {
	nc, err := nats.Connect(url,
		nats.Name("regente-server"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, err
	}
	return &natsTransport{nc: nc}, nil
}

func (t *natsTransport) Publish(subject string, data []byte) error {
	return t.nc.Publish(subject, data)
}

func (t *natsTransport) Subscribe(subject string, handler func([]byte)) error {
	_, err := t.nc.Subscribe(subject, func(m *nats.Msg) { handler(m.Data) })
	return err
}
