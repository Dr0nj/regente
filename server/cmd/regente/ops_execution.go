package main

import (
	"flag"
	"fmt"
	"github.com/Dr0nj/regente-server/pkg/client"
)

func opsExecutions(args []string) error {
	fs := flag.NewFlagSet("ops executions", flag.ContinueOnError)
	conn := opsConn(fs)
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("one instance ID is required")
	}
	raw, err := conn().Executions(fs.Arg(0))
	if err != nil {
		return err
	}
	return printRaw(raw)
}
func opsResolve(args []string, effect bool) error {
	fs := flag.NewFlagSet("ops resolve", flag.ContinueOnError)
	conn := opsConn(fs)
	generation := fs.Int("generation", -1, "current post-action generation")
	fence := fs.Int64("fence", 0, "current execution fence")
	key := fs.String("key", "", "idempotency key (reuse for retrying this request)")
	decision := fs.String("decision", "", "verified outcome")
	reason := fs.String("reason", "", "evidence supporting this resolution")
	stopped := fs.Bool("effect-stopped", false, "confirm the external operation is stopped")
	classification := fs.String("classification", "", "why repeating the effect is acceptable")
	risk := fs.Bool("accept-duplicate-risk", false, "accept possible duplicate external effects")
	if err := fs.Parse(reorderArgs(args, "effect-stopped", "accept-duplicate-risk")); err != nil {
		return err
	}
	if fs.NArg() != 1 || *key == "" || *reason == "" || !*stopped {
		return fmt.Errorf("ID, -key, -reason and -effect-stopped are required")
	}
	if effect {
		if *generation < 0 {
			return fmt.Errorf("-generation must match the current post-action")
		}
		raw, err := conn().ResolveEffect(fs.Arg(0), client.EffectResolution{Generation: *generation, Key: *key, Decision: *decision, Reason: *reason, Classification: *classification, EffectStopped: *stopped, DuplicateRiskAccepted: *risk})
		if err != nil {
			return err
		}
		return printRaw(raw)
	}
	if *fence <= 0 {
		return fmt.Errorf("-fence must match the current attempt")
	}
	raw, err := conn().ResolveExecution(fs.Arg(0), client.ExecutionResolution{Fence: *fence, Key: *key, Decision: *decision, Reason: *reason, EffectStopped: *stopped})
	if err != nil {
		return err
	}
	return printRaw(raw)
}
