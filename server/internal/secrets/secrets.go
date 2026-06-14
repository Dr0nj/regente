// Package secrets — H3: fonte de segredos plugável (PAT do GitHub, secret de
// webhook, tokens de agente, chaves SSH, secrets de job).
//
// O objetivo do H3 é tirar segredos do SQLite em claro. O contrato é um
// Provider simples; o default (env + arquivo JSON) já é melhor que persistir em
// claro no banco e serve para dev/local. Para produção, basta implementar
// Provider sobre Vault ou AWS Secrets Manager e somá-lo ao Chain — nenhum
// call-site muda.
//
// Convenção de chave: nomes lógicos em snake_case ("github_token",
// "webhook_secret"). O EnvProvider mapeia para REGENTE_SECRET_<UPPER>.
package secrets

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// Provider resolve um segredo por chave lógica. found=false quando ausente
// (não é erro — o Chain tenta o próximo provider).
type Provider interface {
	Get(key string) (value string, found bool)
	Name() string
}

// EnvProvider lê REGENTE_SECRET_<UPPER_KEY> do ambiente.
type EnvProvider struct{}

func (EnvProvider) Name() string { return "env" }

func (EnvProvider) Get(key string) (string, bool) {
	v := os.Getenv("REGENTE_SECRET_" + strings.ToUpper(key))
	if v == "" {
		return "", false
	}
	return v, true
}

// FileProvider lê um JSON simples {"chave":"valor"} de um arquivo (modo 0600
// recomendado). Carregado uma vez; ausência de arquivo = provider vazio.
type FileProvider struct {
	once sync.Once
	path string
	data map[string]string
}

// NewFileProvider — path vazio = provider inerte (sempre miss).
func NewFileProvider(path string) *FileProvider { return &FileProvider{path: path} }

func (f *FileProvider) Name() string { return "file" }

func (f *FileProvider) Get(key string) (string, bool) {
	f.once.Do(func() {
		f.data = map[string]string{}
		if f.path == "" {
			return
		}
		raw, err := os.ReadFile(f.path)
		if err != nil {
			return
		}
		_ = json.Unmarshal(raw, &f.data)
	})
	v, ok := f.data[key]
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// Chain tenta os providers em ordem; o primeiro hit vence.
type Chain struct{ providers []Provider }

func NewChain(p ...Provider) *Chain { return &Chain{providers: p} }

func (c *Chain) Name() string {
	names := make([]string, 0, len(c.providers))
	for _, p := range c.providers {
		names = append(names, p.Name())
	}
	return strings.Join(names, "->")
}

func (c *Chain) Get(key string) (string, bool) {
	for _, p := range c.providers {
		if v, ok := p.Get(key); ok {
			return v, true
		}
	}
	return "", false
}

// GetOr devolve o segredo ou o fallback se ausente.
func (c *Chain) GetOr(key, fallback string) string {
	if v, ok := c.Get(key); ok {
		return v
	}
	return fallback
}

// Default monta o Chain padrão: env tem prioridade sobre o arquivo. Para
// produção, prepende um provider de Vault/AWS Secrets Manager aqui.
func Default(filePath string) *Chain {
	return NewChain(EnvProvider{}, NewFileProvider(filePath))
}
