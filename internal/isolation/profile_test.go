package isolation

import "testing"

func TestProfileForLevel(t *testing.T) {
	tests := []struct {
		level       string
		wantRuntime string
		wantDeny    bool
	}{
		{level: "baseline", wantRuntime: "", wantDeny: false},
		{level: "restricted", wantRuntime: "", wantDeny: true},
		{level: "sandboxed", wantRuntime: "kata-qemu", wantDeny: true},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			got, err := ProfileForLevel(tt.level)
			if err != nil {
				t.Fatalf("ProfileForLevel() error = %v", err)
			}
			if got.RuntimeClassName != tt.wantRuntime {
				t.Fatalf("RuntimeClassName = %q, want %q", got.RuntimeClassName, tt.wantRuntime)
			}
			if got.DefaultDenyNetworkPolicy != tt.wantDeny {
				t.Fatalf("DefaultDenyNetworkPolicy = %v, want %v", got.DefaultDenyNetworkPolicy, tt.wantDeny)
			}
		})
	}
}

func TestProfileForLevelRejectsUnknown(t *testing.T) {
	if _, err := ProfileForLevel("unknown"); err == nil {
		t.Fatal("expected error for unknown isolation level")
	}
}
