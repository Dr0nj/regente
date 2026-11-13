package db

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Verificação incremental da trilha de auditoria.
//
// Por que existe (produção, 2026-11-13): o boot reverificava a cadeia INTEIRA
// antes de servir — hash e assinatura Ed25519 de cada registro desde o primeiro.
// Com 2,7 milhões de registros numa VPS com CPU limitada foram 27m49s de
// indisponibilidade a CADA restart, e o custo só cresce com o histórico.
//
// Como funciona: depois de verificar a cadeia até o registro N, o server grava
// um CHECKPOINT {stream, seq=N, hash(N)} assinado com a chave privada da
// auditoria, num arquivo ao lado da chave (fora do banco). No boot seguinte só
// o trecho posterior a N é verificado antes de servir, ancorado no hash de N.
//
// O que isto preserva e o que muda:
//   - Quem altera o banco sem a chave privada não consegue forjar o checkpoint;
//     um checkpoint inválido ou de outro stream cai na verificação integral, e
//     um checkpoint À FRENTE da cabeça do banco (rollback para backup antigo ou
//     truncamento) recusa a partida.
//   - Registros posteriores ao checkpoint continuam verificados ANTES de servir.
//   - Registros anteriores passam a ser reverificados em SEGUNDO PLANO depois do
//     boot (VerifyAuditBackground), com ritmo limitado. Adulteração neles é
//     detectada depois de servir, não antes; ao detectar, o checkpoint é apagado
//     e o processo encerra — o boot seguinte faz a verificação integral e recusa
//     a partida, exatamente como antes.
//   - "-audit-verify" e "-audit-startup-verify=full" continuam fazendo a
//     verificação integral sob demanda.

const auditCheckpointDomain = "regente-audit-startup-checkpoint-v1"

type auditStartupCheckpoint struct {
	Stream    string `json:"stream"`
	Seq       int64  `json:"seq"`
	Hash      string `json:"hash"`
	Signature string `json:"signature"`
}

func auditCheckpointMessage(stream string, seq int64, hash string) []byte {
	return []byte(auditCheckpointDomain + "\n" + stream + "\n" + strconv.FormatInt(seq, 10) + "\n" + hash)
}

func (d *DB) writeAuditCheckpoint(path, stream string, seq int64, hash string) error {
	cp := auditStartupCheckpoint{Stream: stream, Seq: seq, Hash: hash,
		Signature: hex.EncodeToString(ed25519.Sign(d.audit.key, auditCheckpointMessage(stream, seq, hash)))}
	raw, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	// Nome temporário ÚNICO: dois nós sobre o mesmo banco gravam o checkpoint ao
	// mesmo tempo na partida; com um ".tmp" fixo um atropelava o arquivo do outro.
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp, 0600)
	}
	// No Windows o rename falha enquanto outro processo troca o mesmo destino.
	for attempt := 0; err == nil; attempt++ {
		if err = os.Rename(tmp, path); err == nil {
			return nil
		}
		if attempt >= 20 {
			break
		}
		err = nil
		time.Sleep(15 * time.Millisecond)
	}
	_ = os.Remove(tmp)
	return err
}

// readAuditCheckpoint devolve nil quando não há checkpoint utilizável: ausente,
// corrompido ou com assinatura inválida. Nesses casos vale a verificação integral.
func (d *DB) readAuditCheckpoint(path string) *auditStartupCheckpoint {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cp auditStartupCheckpoint
	if json.Unmarshal(raw, &cp) != nil || cp.Seq < 0 {
		return nil
	}
	sig, err := hex.DecodeString(cp.Signature)
	if err != nil || !ed25519.Verify(d.audit.key.Public().(ed25519.PublicKey), auditCheckpointMessage(cp.Stream, cp.Seq, cp.Hash), sig) {
		return nil
	}
	return &cp
}

func (d *DB) auditHead() (stream string, seq int64, hash string, key ed25519.PublicKey, err error) {
	var public string
	if err = d.QueryRow("SELECT stream,seq,hash,public_key FROM audit_head WHERE id=1").Scan(&stream, &seq, &hash, &public); err != nil {
		return
	}
	if d.audit != nil && public != hex.EncodeToString(d.audit.key.Public().(ed25519.PublicKey)) {
		err = fmt.Errorf("audit public key altered")
		return
	}
	raw, e := hex.DecodeString(public)
	if e != nil || len(raw) != ed25519.PublicKeySize {
		err = fmt.Errorf("invalid audit public key")
		return
	}
	return stream, seq, hash, ed25519.PublicKey(raw), nil
}

// auditVerifyChunk — registros por consulta. Consultas curtas em vez de uma
// transação de leitura atravessando a tabela inteira: uma leitura longa impede
// o checkpoint do WAL no SQLite enquanto dura.
const auditVerifyChunk = 2000

// verifyAuditRange verifica (fromSeq, toSeq], ancorado em fromHash, e confere
// que cada registro tem sua linha de entrega. pace > 0 faz a rotina dormir
// pace× o tempo gasto em cada bloco (verificação em segundo plano).
func (d *DB) verifyAuditRange(ctx context.Context, stream string, key ed25519.PublicKey, fromSeq int64, fromHash string, toSeq int64, toHash string, pace int) error {
	cursor, previous := fromSeq, fromHash
	for cursor < toSeq {
		if err := ctx.Err(); err != nil {
			return err
		}
		started := time.Now()
		first := cursor
		upper := cursor + auditVerifyChunk
		if upper > toSeq {
			upper = toSeq
		}
		rows, err := d.Query("SELECT seq,ts,previous,payload,hash,signature FROM security_audit WHERE seq>? AND seq<=? ORDER BY seq", cursor, upper)
		if err != nil {
			return err
		}
		for rows.Next() {
			r := AuditRecord{Stream: stream}
			if err = rows.Scan(&r.Seq, &r.TS, &r.Previous, &r.Payload, &r.Hash, &r.Signature); err != nil {
				rows.Close()
				return err
			}
			if err = VerifyAuditRecord(r, key, previous, cursor+1); err != nil {
				rows.Close()
				return err
			}
			cursor, previous = r.Seq, r.Hash
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if cursor != upper {
			return fmt.Errorf("audit integrity mismatch at sequence %d", cursor+1)
		}
		var delivered int64
		if err = d.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE seq>? AND seq<=?", first, upper).Scan(&delivered); err != nil {
			return err
		}
		if delivered != upper-first {
			return fmt.Errorf("audit delivery gap detected")
		}
		if pace > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(pace) * time.Since(started)):
			}
		}
	}
	if previous != toHash {
		return fmt.Errorf("audit head mismatch")
	}
	return nil
}

// AuditStartup descreve o que o boot verificou e o que ficou para o segundo plano.
type AuditStartup struct {
	// Deferred — registros (0..Deferred] aceitos pelo checkpoint assinado e ainda
	// não reverificados neste processo. Zero = a cadeia inteira foi verificada.
	Deferred int64
	// Head — último registro verificado antes de servir.
	Head           int64
	checkpointPath string
	deferredHash   string
}

// EnableAuditIncremental liga a auditoria verificando, antes de servir, só o
// trecho posterior ao último checkpoint assinado. Sem checkpoint utilizável faz
// a verificação integral (primeiro boot depois do upgrade) e grava um.
func (d *DB) EnableAuditIncremental(path string, capacity int64) (AuditStartup, error) {
	out := AuditStartup{checkpointPath: path + ".checkpoint"}
	if err := d.prepareAudit(path, capacity); err != nil {
		return out, err
	}
	stream, headSeq, headHash, key, err := d.auditHead()
	if err != nil {
		return out, err
	}
	out.Head = headSeq
	cp := d.readAuditCheckpoint(out.checkpointPath)
	if cp != nil && cp.Stream == stream && cp.Seq > headSeq {
		// O banco está ATRÁS de um ponto que este server já verificou e assinou:
		// restauração de backup antigo ou truncamento. Não é caso de seguir.
		return out, fmt.Errorf("audit ledger is behind the signed startup checkpoint (rollback or truncation); reconcile it, verify with -audit-verify, and only then remove %s", out.checkpointPath)
	}
	if cp == nil || cp.Stream != stream {
		if err = d.VerifyAudit(); err != nil {
			return out, err
		}
		return out, d.writeAuditCheckpoint(out.checkpointPath, stream, headSeq, headHash)
	}
	if cp.Seq > 0 {
		var stored string
		if err = d.QueryRow("SELECT hash FROM security_audit WHERE seq=?", cp.Seq).Scan(&stored); err != nil || stored != cp.Hash {
			return out, fmt.Errorf("audit ledger does not match the signed startup checkpoint at sequence %d", cp.Seq)
		}
	} else if cp.Hash != "" {
		return out, fmt.Errorf("invalid empty audit checkpoint")
	}
	if err = d.verifyAuditRange(context.Background(), stream, key, cp.Seq, cp.Hash, headSeq, headHash, 0); err != nil {
		return out, err
	}
	out.Deferred, out.deferredHash = cp.Seq, cp.Hash
	return out, d.writeAuditCheckpoint(out.checkpointPath, stream, headSeq, headHash)
}

// ErrAuditBackground embrulha uma falha de integridade achada em segundo plano.
var ErrAuditBackground = errors.New("audit ledger failed background verification")

// VerifyAuditBackground reverifica, com ritmo limitado, o trecho que o boot
// aceitou pelo checkpoint e depois mantém o checkpoint avançando a cada
// `every`, para o próximo boot ter pouco a verificar. Só retorna por
// cancelamento do contexto ou por falha de INTEGRIDADE — nesse caso o
// checkpoint já foi removido e o chamador deve encerrar o processo.
func (d *DB) VerifyAuditBackground(ctx context.Context, start AuditStartup, every time.Duration) error {
	fail := func(err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = os.Remove(start.checkpointPath)
		return fmt.Errorf("%w: %v", ErrAuditBackground, err)
	}
	stream, _, _, key, err := d.auditHead()
	if err != nil {
		return fail(err)
	}
	if start.Deferred > 0 {
		// pace 3 = no máximo ~25% de um núcleo.
		if err = d.verifyAuditRange(ctx, stream, key, 0, "", start.Deferred, start.deferredHash, 3); err != nil {
			return fail(err)
		}
	}
	verifiedSeq := start.Head
	var verifiedHash string
	if verifiedSeq > 0 {
		if err = d.QueryRow("SELECT hash FROM security_audit WHERE seq=?", verifiedSeq).Scan(&verifiedHash); err != nil {
			return fail(err)
		}
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		_, headSeq, _, _, err := d.auditHead()
		if err != nil {
			continue // banco ocupado ou fechando: tenta na próxima volta
		}
		if headSeq == verifiedSeq {
			continue
		}
		if headSeq < verifiedSeq {
			return fail(fmt.Errorf("audit head moved backwards"))
		}
		// O hash final é conferido contra a LINHA do alvo, não contra a cabeça,
		// que continua andando enquanto o trecho é lido.
		var target string
		if err = d.QueryRow("SELECT hash FROM security_audit WHERE seq=?", headSeq).Scan(&target); err != nil {
			continue
		}
		if err = d.verifyAuditRange(ctx, stream, key, verifiedSeq, verifiedHash, headSeq, target, 3); err != nil {
			return fail(err)
		}
		verifiedSeq, verifiedHash = headSeq, target
		_ = d.writeAuditCheckpoint(start.checkpointPath, stream, verifiedSeq, verifiedHash)
	}
}
