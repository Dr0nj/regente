package db

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Só a declaração corrente é contrato; versões históricas/fixtures são legítimas.
func checkRuntimeSchemaDoc(body string, actual int) error {
	re := regexp.MustCompile(`(?m)^Current runtime schema: \*\*(\d+)\*\*; supported range: \*\*\[(\d+),(\d+)\]\*\*\.[\r]?$`)
	matches := re.FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		return fmt.Errorf("esperava uma declaração corrente de schema, encontrou %d", len(matches))
	}
	want := []int{actual, MinSupportedSchema, MaxSupportedSchema}
	for i, value := range matches[0][1:] {
		got, err := strconv.Atoi(value)
		if err != nil || got != want[i] {
			return fmt.Errorf("contrato documental de schema diverge: campo %d=%s, esperado %d", i, value, want[i])
		}
	}
	return nil
}

func TestMigrationRunbookContract(t *testing.T) {
	d, _ := migrationTestDB(t, SQLite)
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}
	var actual int
	if err := d.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "integration-baseline.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRuntimeSchemaDoc(string(body), actual); err != nil {
		t.Fatal(err)
	}
	valid := fmt.Sprintf("Current runtime schema: **%d**; supported range: **[%d,%d]**.", actual, MinSupportedSchema, MaxSupportedSchema)
	for name, bad := range map[string]string{
		"missing":      "Historical schema 24",
		"duplicate":    valid + "\n" + valid,
		"stale_schema": strings.Replace(valid, fmt.Sprintf("**%d**", actual), "**0**", 1),
		"wrong_range":  strings.Replace(valid, fmt.Sprintf("[%d,%d]", MinSupportedSchema, MaxSupportedSchema), "[0,999]", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if checkRuntimeSchemaDoc(bad, actual) == nil {
				t.Fatal("aceitou contrato corrente ausente/errado")
			}
		})
	}
	if err := checkRuntimeSchemaDoc(valid+"\nHistorical schema 22 and 24 remain valid references.\n", actual); err != nil {
		t.Fatal(err)
	}
}
