// Package policy — D-10 Policy as Code.
//
// Regras OBRIGATÓRIAS de governança sobre as definitions, versionadas JUNTO
// dos jobs: <workspace>/policies.yaml. Como o arquivo vive no repo GitOps, a
// política promove/versiona/reverte com o mesmo fluxo dos jobs — e a session
// de Design valida contra a política DO PRÓPRIO CLONE (mudar política + jobs
// no mesmo publish é atômico).
//
//	# policies.yaml
//	enforcement: error        # error (bloqueia publish) | warn (avisa) | off
//	exemptFolders: [sandbox]  # folders fora da política
//	rules:
//	  requireSLA: true          # todo job precisa de sla (duração e/ou deadline)
//	  requireRetries: 1         # retries mínimo
//	  requireDescription: true  # schedule.description não-vazia (o "owner doc")
//	  requireCalendar: false    # todo job precisa de calendar
//	  idPattern: "^[a-z][a-z0-9_-]*$"
//	  allowedJobTypes: [COMMAND, HTTP]   # vazio = todos
//	  maxRetries: 10
//	  forbidDryRun: false       # dryRun proibido (ex.: política de prod)
//
// Avaliação é FUNÇÃO PURA sobre []JobDefinition → []Violation; quem chama
// decide o efeito (publish 422, teste CLI exit 1, endpoint read-only).
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Dr0nj/regente-server/internal/domain"
	"gopkg.in/yaml.v3"
)

type Rules struct {
	RequireSLA         bool     `yaml:"requireSLA" json:"requireSLA,omitempty"`
	RequireRetries     int      `yaml:"requireRetries" json:"requireRetries,omitempty"`
	RequireDescription bool     `yaml:"requireDescription" json:"requireDescription,omitempty"`
	RequireCalendar    bool     `yaml:"requireCalendar" json:"requireCalendar,omitempty"`
	IDPattern          string   `yaml:"idPattern" json:"idPattern,omitempty"`
	AllowedJobTypes    []string `yaml:"allowedJobTypes" json:"allowedJobTypes,omitempty"`
	MaxRetries         int      `yaml:"maxRetries" json:"maxRetries,omitempty"`
	ForbidDryRun       bool     `yaml:"forbidDryRun" json:"forbidDryRun,omitempty"`
}

type Policy struct {
	Enforcement   string   `yaml:"enforcement" json:"enforcement"` // error | warn | off
	ExemptFolders []string `yaml:"exemptFolders" json:"exemptFolders,omitempty"`
	Rules         Rules    `yaml:"rules" json:"rules"`

	idRe *regexp.Regexp // compilado no Load
}

type Violation struct {
	DefID   string `json:"defId"`
	Folder  string `json:"folder"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// FileName — nome canônico do arquivo na raiz do workspace.
const FileName = "policies.yaml"

// Load lê <root>/policies.yaml. Sem arquivo → (nil, nil): ausência de política
// não é erro, é "sem governança configurada". YAML inválido ou idPattern que
// não compila SÃO erro (política quebrada não pode virar política-que-não-vale).
func Load(root string) (*Policy, error) {
	raw, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p Policy
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true) // parse estrito: typo em regra ≠ regra silenciosamente ignorada
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	switch p.Enforcement {
	case "", "off", "warn", "error":
	default:
		return nil, fmt.Errorf("%s: enforcement must be error|warn|off (got %q)", FileName, p.Enforcement)
	}
	if p.Rules.IDPattern != "" {
		re, err := regexp.Compile(p.Rules.IDPattern)
		if err != nil {
			return nil, fmt.Errorf("%s: idPattern: %w", FileName, err)
		}
		p.idRe = re
	}
	return &p, nil
}

// Active — a política tem efeito? (nil ou enforcement off/vazio = não).
func (p *Policy) Active() bool {
	return p != nil && (p.Enforcement == "warn" || p.Enforcement == "error")
}

// Blocking — violações devem BLOQUEAR o write?
func (p *Policy) Blocking() bool { return p != nil && p.Enforcement == "error" }

// Validate avalia todas as defs contra as regras. Determinístico e ordenado
// (ordem de entrada); folder isenta pula todas as regras.
func (p *Policy) Validate(defs []domain.JobDefinition) []Violation {
	if !p.Active() {
		return nil
	}
	exempt := map[string]bool{}
	for _, f := range p.ExemptFolders {
		exempt[f] = true
	}
	var out []Violation
	add := func(d domain.JobDefinition, rule, msg string) {
		out = append(out, Violation{DefID: d.ID, Folder: d.Team, Rule: rule, Message: msg})
	}
	for _, d := range defs {
		if exempt[d.Team] {
			continue
		}
		r := p.Rules
		if r.RequireSLA && (d.SLA == nil || (d.SLA.ExpectedDurationMin <= 0 && d.SLA.DeadlineHM == "")) {
			add(d, "requireSLA", "job without an SLA (expectedDurationMin or deadlineHM)")
		}
		if r.RequireRetries > 0 && d.Retries < r.RequireRetries {
			add(d, "requireRetries", fmt.Sprintf("retries=%d below the minimum %d", d.Retries, r.RequireRetries))
		}
		if r.RequireDescription && strings.TrimSpace(d.Schedule.Description) == "" {
			add(d, "requireDescription", "job without a description (owner/purpose)")
		}
		if r.RequireCalendar && d.Calendar == "" && len(d.Calendars) == 0 {
			add(d, "requireCalendar", "job without a calendar")
		}
		if p.idRe != nil && !p.idRe.MatchString(d.ID) {
			add(d, "idPattern", fmt.Sprintf("id %q does not match %q", d.ID, r.IDPattern))
		}
		if len(r.AllowedJobTypes) > 0 && !containsFold(r.AllowedJobTypes, d.JobType) {
			add(d, "allowedJobTypes", fmt.Sprintf("jobType %q outside the allowed list %v", d.JobType, r.AllowedJobTypes))
		}
		if r.MaxRetries > 0 && d.Retries > r.MaxRetries {
			add(d, "maxRetries", fmt.Sprintf("retries=%d above the cap %d", d.Retries, r.MaxRetries))
		}
		if r.ForbidDryRun && d.DryRun {
			add(d, "forbidDryRun", "dryRun forbidden by the policy")
		}
	}
	return out
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}
