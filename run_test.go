package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		wantErr    error
		wantStdout string
		wantStderr string
	}{
		{"-h prints the usage", []string{"-h"}, nil, "", "Usage:"},
		{"-version prints the version", []string{"-version"}, nil, version() + "\n", ""},
		{"a bad flag is a usage error", []string{"-nope"}, errUsage, "", "flag provided but not defined: -nope"},
		{"an argument is a usage error", []string{"extra"}, errUsage, "", `unexpected argument "extra"`},
		{"-listen needs host:port", []string{"-listen", "8888"}, errUsage, "", `-listen "8888": want host:port`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			err := run(t.Context(), tt.args, &stdout, &stderr)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run = %v, want %v", err, tt.wantErr)
			}
			if stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRun_servesUntilStopped(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	// Stopped before it starts: run binds, says so, and shuts down at once.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := run(ctx, []string{"-listen", "127.0.0.1:0"}, &stdout, &stderr)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want it to wrap %v", err, context.Canceled)
	}
	if !strings.Contains(stderr.String(), "egress: listening on 127.0.0.1:") {
		t.Errorf("stderr = %q, want the address it listened on", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", stdout.String())
	}
}

// The two tests below set the environment, so they cannot run in parallel.

func TestRun_environmentSetsTheDefault(t *testing.T) {
	t.Setenv("EGRESS_LISTEN", "nowhere")
	var stdout, stderr bytes.Buffer
	if err := run(t.Context(), nil, &stdout, &stderr); !errors.Is(err, errUsage) {
		t.Fatalf("run = %v, want %v", err, errUsage)
	}
	if want := `-listen "nowhere"`; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func TestRun_flagBeatsEnvironment(t *testing.T) {
	t.Setenv("EGRESS_LISTEN", "nowhere")
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := run(ctx, []string{"-listen", "127.0.0.1:0"}, &stdout, &stderr); !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want it to wrap %v (stderr: %s)", err, context.Canceled, stderr.String())
	}
}
