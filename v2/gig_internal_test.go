package gig

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type testCfg struct {
	Name string `yaml:"name"`
}

func TestLoadOptionError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("option error")
	_, err := Load[testCfg](
		context.Background(),
		strings.NewReader("{}\n"),
		func(l *loader) error {
			return wantErr
		},
	)
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("Load() error = %v, want %v", err, wantErr)
	}
}
