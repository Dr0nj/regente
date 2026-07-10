package domain

import (
	"errors"
	"fmt"
	"strings"
)

/* ──────────────────────────────────────────────────────────────
   F12 → ADV-1 — Validação de actionConfig por jobType
   ──────────────────────────────────────────────────────────────
   A regra por tipo vive no REGISTRY declarativo (typeschema.go):
   obrigatórios, kinds, enums, aliases e campos desconhecidos.
   Aqui fica só o despacho: tipo conhecido → schema dedicado;
   tipo desconhecido → params livres (mundo aberto preservado).
   ────────────────────────────────────────────────────────────── */

func ValidateActionConfig(def JobDefinition) error { return validateAC(def, true) }

// ValidateActionConfigDraft — versão de RASCUNHO: mesmas checagens de kind,
// enum e campo desconhecido (pega typo na hora), mas SEM cobrar obrigatórios
// nem regras cross-field — um card recém-arrastado da palette ainda não tem
// command/url/etc. Os obrigatórios são cobrados no publish (strict).
func ValidateActionConfigDraft(def JobDefinition) error { return validateAC(def, false) }

func validateAC(def JobDefinition, strict bool) error {
	jt := strings.TrimSpace(def.JobType)
	if jt == "" {
		return errors.New("jobType required")
	}
	s, known := SchemaFor(jt)
	if !known {
		// jobType desconhecido: aceita actionConfig livre, sem validação rígida
		// (só roda se um agente anunciar a capability; senão WAIT_AGENT).
		return nil
	}
	cfg := def.Params
	if cfg == nil {
		if !strict {
			return nil
		}
		// nil = "sem params". Só é erro se o tipo tem campo obrigatório.
		for _, f := range s.Fields {
			if f.Required {
				return fmt.Errorf("%s.%s required", s.Type, f.Name)
			}
		}
		return nil
	}
	return validateAgainstSchema(s, cfg, strict)
}

// ValidateDefinition — validação ESTRITA: shape básico + schema dedicado por
// tipo COM obrigatórios (ADV-1). É o gate do publish e dos writes diretos no
// workspace publicado (definitions/massupdate-apply/CLI test).
func ValidateDefinition(def JobDefinition) error {
	if err := validateDefShape(def); err != nil {
		return err
	}
	if err := ValidateActionConfig(def); err != nil {
		return fmt.Errorf("actionConfig: %w", err)
	}
	return nil
}

// ValidateDefinitionDraft — validação de RASCUNHO (save de Design session,
// jobs-as-code apply, bulk/massupdate de session): shape básico + kinds/enums/
// campos desconhecidos, sem cobrar params obrigatórios. Ver ValidateActionConfigDraft.
func ValidateDefinitionDraft(def JobDefinition) error {
	if err := validateDefShape(def); err != nil {
		return err
	}
	if err := ValidateActionConfigDraft(def); err != nil {
		return fmt.Errorf("actionConfig: %w", err)
	}
	return nil
}

func validateDefShape(def JobDefinition) error {
	if strings.TrimSpace(def.ID) == "" {
		return errors.New("id required")
	}
	if strings.TrimSpace(def.Label) == "" {
		return errors.New("label required")
	}
	if strings.TrimSpace(def.Team) == "" {
		return errors.New("team (folder) required")
	}
	return nil
}
