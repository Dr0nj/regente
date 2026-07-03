package api

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Dr0nj/regente-server/internal/domain"
)

/* ──────────────────────────────────────────────────────────────
   F12 — Validação de actionConfig por jobType
   ──────────────────────────────────────────────────────────────
   Espelha JobActionConfigEditor.tsx do frontend. Mantém shape
   mínimo obrigatório por type. Para CHOICE/PARALLEL aceita
   branches como []interface{} (frontend serializa JSON).
   ────────────────────────────────────────────────────────────── */

func validateActionConfig(def domain.JobDefinition) error {
	cfg := def.Params
	jt := strings.ToUpper(def.JobType)
	switch jt {
	case "HTTP":
		if cfg == nil {
			return errors.New("HTTP requires actionConfig")
		}
		if !isNonEmptyString(cfg["url"]) {
			return errors.New("HTTP.url required")
		}
		if v, ok := cfg["method"]; ok {
			if s, ok := v.(string); !ok || !isHTTPMethod(s) {
				return errors.New("HTTP.method must be GET|POST|PUT|PATCH|DELETE")
			}
		}
	case "LAMBDA":
		if cfg == nil || !isNonEmptyString(cfg["functionName"]) {
			return errors.New("LAMBDA.functionName required")
		}
	case "BATCH":
		if cfg == nil || !isNonEmptyString(cfg["jobQueue"]) || !isNonEmptyString(cfg["jobDefinition"]) {
			return errors.New("BATCH requires jobQueue + jobDefinition")
		}
	case "GLUE":
		if cfg == nil || !isNonEmptyString(cfg["jobName"]) {
			return errors.New("GLUE.jobName required")
		}
	case "STEP_FUNCTION":
		if cfg == nil || !isNonEmptyString(cfg["stateMachineArn"]) {
			return errors.New("STEP_FUNCTION.stateMachineArn required")
		}
	case "FILE_WATCH", "FILEWATCH":
		if cfg == nil || !isNonEmptyString(cfg["path"]) {
			return errors.New("FILE_WATCH.path required (caminho do arquivo no host do agente)")
		}
	case "DATABASE", "DB":
		if cfg == nil || !isNonEmptyString(cfg["driver"]) || !isNonEmptyString(cfg["dsn"]) || !isNonEmptyString(cfg["sql"]) {
			return errors.New("DATABASE requires driver + dsn + sql")
		}
		if d, _ := cfg["driver"].(string); !isDBDriver(d) {
			return errors.New("DATABASE.driver must be postgres|mysql|sqlite")
		}
	case "WAIT":
		if cfg == nil {
			return nil // wait sem config = noop, ok
		}
		_, hasSec := cfg["seconds"]
		_, hasUntil := cfg["until"]
		if !hasSec && !hasUntil {
			return errors.New("WAIT requires seconds or until")
		}
	case "CHOICE":
		if cfg == nil || !isNonEmptyString(cfg["expression"]) {
			return errors.New("CHOICE.expression required")
		}
	case "PARALLEL":
		if cfg == nil {
			return errors.New("PARALLEL requires branches")
		}
		br, ok := cfg["branches"].([]interface{})
		if !ok || len(br) == 0 {
			return errors.New("PARALLEL.branches must be non-empty array")
		}
	case "":
		return errors.New("jobType required")
	default:
		// jobType desconhecido: aceita actionConfig livre, sem validação rígida.
		return nil
	}
	return nil
}

func isNonEmptyString(v interface{}) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != ""
}

func isHTTPMethod(s string) bool {
	switch strings.ToUpper(s) {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

func isDBDriver(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "postgres", "postgresql", "pg", "pgx", "mysql", "mariadb", "sqlite", "sqlite3":
		return true
	}
	return false
}

func validateDefinition(def domain.JobDefinition) error {
	if strings.TrimSpace(def.ID) == "" {
		return errors.New("id required")
	}
	if strings.TrimSpace(def.Label) == "" {
		return errors.New("label required")
	}
	if strings.TrimSpace(def.Team) == "" {
		return errors.New("team (folder) required")
	}
	if err := validateActionConfig(def); err != nil {
		return fmt.Errorf("actionConfig: %w", err)
	}
	return nil
}
