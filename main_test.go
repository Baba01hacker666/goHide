package main

import "testing"

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		argv    []string
		fake    string
		hide    bool
		wipe    bool
		cmd     []string
		wantErr bool
	}{
		{name: "long flag", argv: []string{"--as", "kworker/3:1", "--", "sleep", "3600"},
			fake: "kworker/3:1", cmd: []string{"sleep", "3600"}},
		{name: "short flag alias", argv: []string{"-n", "kworker", "--", "sh", "-c", "x"},
			fake: "kworker", cmd: []string{"sh", "-c", "x"}},
		{name: "hide and wipe", argv: []string{"-n", "x", "--hide", "--wipe-args", "--", "nc", "-l", "-p", "9"},
			fake: "x", hide: true, wipe: true, cmd: []string{"nc", "-l", "-p", "9"}},
		{name: "flags after -- are command args", argv: []string{"-n", "x", "--", "curl", "-sS", "http://x"},
			fake: "x", cmd: []string{"curl", "-sS", "http://x"}},
		{name: "missing separator", argv: []string{"-n", "x", "sleep", "1"}, wantErr: true},
		{name: "missing name", argv: []string{"--", "sleep", "1"}, wantErr: true},
		{name: "empty command", argv: []string{"-n", "x", "--"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseArgs(tt.argv)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got cfg=%+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.fake != tt.fake {
				t.Errorf("fake = %q, want %q", cfg.fake, tt.fake)
			}
			if cfg.hide != tt.hide || cfg.wipe != tt.wipe {
				t.Errorf("hide/wipe = %v/%v, want %v/%v", cfg.hide, cfg.wipe, tt.hide, tt.wipe)
			}
			if len(cfg.cmd) != len(tt.cmd) {
				t.Fatalf("cmd = %v, want %v", cfg.cmd, tt.cmd)
			}
			for i := range tt.cmd {
				if cfg.cmd[i] != tt.cmd[i] {
					t.Errorf("cmd[%d] = %q, want %q", i, cfg.cmd[i], tt.cmd[i])
				}
			}
		})
	}
}
