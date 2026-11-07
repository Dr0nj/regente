package domain

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// Oráculo anterior ao índice, mantido só em teste para comparar o contrato
// completo (expansão legada, AND/OR, ordem, escopo e idempotência).
func normalizeConditionsScan(defs []JobDefinition) []JobDefinition {
	byID := make(map[string]*JobDefinition)
	for i := range defs {
		byID[defs[i].ID] = &defs[i]
	}
	for i := range defs {
		if len(defs[i].Upstream) > 0 {
			expandLegacyUpstream(&defs[i], byID)
		}
	}
	for i := range defs {
		normalizeConditionLogic(&defs[i])
	}
	for i := range defs {
		c := &defs[i]
		c.Upstream = nil
		seen := make(map[string]bool)
		for _, name := range c.ConditionsIn {
			base, ref := SplitCondRef(name)
			for j := range defs {
				p := &defs[j]
				if p.ID == c.ID || seen[p.ID] || !producesBase(p, base) {
					continue
				}
				seen[p.ID] = true
				e := Upstream{From: p.ID, Condition: CondOnSuccess}
				if ref != DateRefOdat {
					e.DateRef = ref
				}
				c.Upstream = append(c.Upstream, e)
			}
		}
	}
	return defs
}

func TestNormalizeConditionsIndexMatchesScan(t *testing.T) {
	rng := rand.New(rand.NewSource(20261107))
	names := []string{"A", "A@prev", "A@STAT", "A@odat", "B", "B@other", "b", "", "@stat"}
	for trial := 0; trial < 100; trial++ {
		seed := make([]JobDefinition, 24)
		for i := range seed {
			d := &seed[i]
			d.ID = fmt.Sprintf("job-%d", i)
			for j := 0; j < rng.Intn(8); j++ {
				d.ConditionsIn = append(d.ConditionsIn, names[rng.Intn(len(names))])
				d.ConditionsOutAdd = append(d.ConditionsOutAdd, names[rng.Intn(len(names))])
			}
			d.Actions = []ActionRule{{Do: "set-condition", Condition: names[rng.Intn(len(names))]}, {Do: "shout", Condition: "B"}}
			if rng.Intn(2) == 0 {
				d.ConditionLogic = &ConditionLogic{Op: "OR", Groups: []CondGroup{
					{Op: "AND", Members: []string{"A@prev", "B"}},
					{Op: "OR", Members: []string{"b", CondTokenTime}},
				}}
			}
			if i > 0 && rng.Intn(2) == 0 {
				d.Upstream = []Upstream{{From: seed[i-1].ID, Condition: []EdgeCondition{CondOnSuccess, CondOnFailure, CondOnComplete, CondAlways}[rng.Intn(4)], DateRef: []DateRef{DateRefOdat, DateRefPrev, DateRefStat}[rng.Intn(3)]}}
			}
		}
		raw, err := json.Marshal(seed)
		if err != nil {
			t.Fatal(err)
		}
		var got, want []JobDefinition
		if err = json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		for pass := 0; pass < 2; pass++ {
			got = NormalizeConditions(got)
			want = normalizeConditionsScan(want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("contrato divergente: caso=%d passe=%d", trial, pass)
			}
		}
	}
}

func TestNormalizeConditionsPreservesProducerOrderAndFirstReference(t *testing.T) {
	defs := []JobDefinition{
		{ID: "first", ConditionsOutAdd: []string{"shared", "also@stat"}},
		{ID: "second", Actions: []ActionRule{{Do: "set-condition", Condition: "shared@stat"}}},
		{ID: "child", ConditionsIn: []string{"shared@prev", "also@stat"}, ConditionsOutAdd: []string{"shared"}},
		{ID: "last", ConditionsOutAdd: []string{"also", "also@stat"}},
	}
	want := []Upstream{
		{From: "first", Condition: CondOnSuccess, DateRef: DateRefPrev},
		{From: "second", Condition: CondOnSuccess, DateRef: DateRefPrev},
		{From: "last", Condition: CondOnSuccess, DateRef: DateRefStat},
	}
	for range 2 {
		if got := NormalizeConditions(defs)[2].Upstream; !reflect.DeepEqual(got, want) {
			t.Fatalf("esperava %v, recebeu %v", want, got)
		}
	}
}

func BenchmarkNormalizeConditionsLargeDaily(b *testing.B) {
	const jobs = 28_800
	seed := make([]JobDefinition, jobs)
	for i := range seed {
		seed[i] = JobDefinition{ID: fmt.Sprintf("job-%05d", i), ConditionsOutAdd: []string{fmt.Sprintf("condition-%05d", i)}}
		if i%10 != 0 {
			seed[i].ConditionsIn = seed[i-1].ConditionsOutAdd
		}
		if i%10 > 4 {
			seed[i].ConditionsIn = append([]string{seed[i-1].ConditionsOutAdd[0]}, seed[i-5].ConditionsOutAdd[0])
		}
	}
	defs := make([]JobDefinition, jobs)
	b.ResetTimer()
	for range b.N {
		copy(defs, seed)
		NormalizeConditions(defs)
	}
}
