package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunMissingCommand(t *testing.T) {
	if err := run(context.Background(), nil); err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
}

func TestRunUnknownCommand(t *testing.T) {
	err := run(context.Background(), []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		if err := run(context.Background(), args); err != nil {
			t.Errorf("run(%v) = %v, want nil", args, err)
		}
	}
}

func TestCmdUpRejectsArguments(t *testing.T) {
	err := cmdUp(context.Background(), []string{"extra"})
	if err == nil {
		t.Fatal("expected error for unexpected argument, got nil")
	}
	if !strings.Contains(err.Error(), "takes no arguments") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdUpUnknownFlag(t *testing.T) {
	err := cmdUp(context.Background(), []string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag, got nil")
	}
}

func TestCmdUpHelp(t *testing.T) {
	if err := cmdUp(context.Background(), []string{"-h"}); err != nil {
		t.Errorf("cmdUp -h = %v, want nil", err)
	}
}

func TestCmdDownInvalidSteps(t *testing.T) {
	err := cmdDown(context.Background(), []string{"abc"})
	if err == nil {
		t.Fatal("expected error for invalid steps, got nil")
	}
	if !strings.Contains(err.Error(), "invalid steps") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseSteps(t *testing.T) {
	cases := []struct {
		args    []string
		want    int
		wantErr bool
	}{
		{nil, 1, false},
		{[]string{}, 1, false},
		{[]string{"3"}, 3, false},
		{[]string{"0"}, 0, true},
		{[]string{"-1"}, 0, true},
		{[]string{"abc"}, 0, true},
		{[]string{"1", "2"}, 0, true},
	}
	for _, c := range cases {
		got, err := parseSteps(c.args)
		if c.wantErr != (err != nil) {
			t.Errorf("parseSteps(%v) error = %v, wantErr %v", c.args, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("parseSteps(%v) = %d, want %d", c.args, got, c.want)
		}
	}
}
