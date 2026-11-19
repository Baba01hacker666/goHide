package main

import (
	"reflect"
	"regexp"
	"testing"
)

func TestCommand(t *testing.T) {
	tests := []struct {
		name    string
		argv    []string
		want    []string
		wantErr bool
	}{
		{name: "plain command", argv: []string{"sleep", "1"}, want: []string{"sleep", "1"}},
		{name: "command with flags", argv: []string{"nmap", "-sCV", "-F", "host"}, want: []string{"nmap", "-sCV", "-F", "host"}},
		{name: "leading separator stripped", argv: []string{"--", "curl", "-sS", "url"}, want: []string{"curl", "-sS", "url"}},
		{name: "no command", argv: nil, wantErr: true},
		{name: "only separator", argv: []string{"--"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := command(tt.argv)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("command() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestZapperArgs(t *testing.T) {
	t.Setenv("GOHIDE_FOLLOW", "")
	t.Setenv("GOHIDE_KEEP_ENV", "")
	t.Setenv("GOHIDE_PID", "")

	got := zapperArgs("/bin/zapper", "[kworker/3:1]", []string{"nmap", "-sCV", "host"})
	want := []string{"/bin/zapper", "-a", "[kworker/3:1]", "--", "nmap", "-sCV", "host"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("zapperArgs() = %v, want %v", got, want)
	}

	t.Setenv("GOHIDE_FOLLOW", "1")
	t.Setenv("GOHIDE_KEEP_ENV", "1")
	t.Setenv("GOHIDE_PID", "300")
	got = zapperArgs("/bin/zapper", "x", []string{"sleep", "1"})
	want = []string{"/bin/zapper", "-a", "x", "-f", "-E", "-n", "300", "--", "sleep", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("zapperArgs() with env = %v, want %v", got, want)
	}
}

func TestKworkerName(t *testing.T) {
	re := regexp.MustCompile(`^\[kworker/\d+:\d+(-[a-z_]+)?\]$`)
	for i := 0; i < 100; i++ {
		if n := kworkerName(); !re.MatchString(n) {
			t.Fatalf("kworkerName() = %q, want kworker-shaped name", n)
		}
	}
}
