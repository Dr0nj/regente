package journal

import "context"

type processKey struct{}

func WithProcessRecorder(ctx context.Context, record func(int) error) context.Context {
	return context.WithValue(ctx, processKey{}, record)
}

// PID é apenas diagnóstico; não autoriza replay nem demonstra propriedade de um processo.
func RecordProcess(ctx context.Context, pid int) error {
	if f, ok := ctx.Value(processKey{}).(func(int) error); ok {
		return f(pid)
	}
	return nil
}
