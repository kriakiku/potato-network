package config

import (
	"net"
	"testing"
)

func TestFromEnvProfileDefaults(t *testing.T) {
	t.Setenv("POTATONETWORK_API_TOKEN", "")
	t.Setenv("POTATONETWORK_API_TOKEN_FILE", "")
	t.Setenv("POTATONETWORK_PROFILE_COUNTRY", "bd")
	t.Setenv("POTATONETWORK_PROFILE_TIER", "")
	t.Setenv("POTATONETWORK_SHAPE_EXCLUDE", "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProfileCountry != "BD" {
		t.Fatalf("country=%q", cfg.ProfileCountry)
	}
	if cfg.ProfileTier != "typical" {
		t.Fatalf("tier=%q", cfg.ProfileTier)
	}
}

func TestFromEnvPassthroughWhenNoCountry(t *testing.T) {
	t.Setenv("POTATONETWORK_PROFILE_COUNTRY", "")
	t.Setenv("POTATONETWORK_PROFILE_TIER", "poor")
	t.Setenv("POTATONETWORK_SHAPE_EXCLUDE", "")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProfileCountry != "" || cfg.ProfileTier != "" {
		t.Fatalf("expected empty boot profile, got %s/%s", cfg.ProfileCountry, cfg.ProfileTier)
	}
}

func TestFromEnvShapeExclude(t *testing.T) {
	t.Setenv("POTATONETWORK_SHAPE_EXCLUDE", "10.0.0.0/8, 1.2.3.4")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ShapeExclude) != 2 {
		t.Fatalf("len=%d", len(cfg.ShapeExclude))
	}
}

func TestFromEnvShapeExcludeInvalid(t *testing.T) {
	t.Setenv("POTATONETWORK_SHAPE_EXCLUDE", "not-an-ip")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error")
	}
}

func TestFromEnvTLSInsecure(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"true", true},
		{"TRUE", false},
		{"1", false},
		{"yes", false},
		{"false", false},
	}
	for _, tc := range cases {
		t.Setenv("POTATONETWORK_TLS_INSECURE", tc.val)
		t.Setenv("POTATONETWORK_SHAPE_EXCLUDE", "")
		cfg, err := FromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.TLSInsecure != tc.want {
			t.Fatalf("val=%q got %v want %v", tc.val, cfg.TLSInsecure, tc.want)
		}
	}
}

func TestParseIPNets(t *testing.T) {
	cases := []struct {
		in      string
		want    []string // CIDR strings
		wantErr bool
	}{
		{"", nil, false},
		{"  ", nil, false},
		{"1.2.3.4", []string{"1.2.3.4/32"}, false},
		{"10.0.0.0/8", []string{"10.0.0.0/8"}, false},
		{"10.0.0.0/8,1.2.3.4", []string{"10.0.0.0/8", "1.2.3.4/32"}, false},
		{"10.0.0.0/8 1.2.3.4\t192.168.1.0/24", []string{"10.0.0.0/8", "1.2.3.4/32", "192.168.1.0/24"}, false},
		{"garbage", nil, true},
		{"2001:db8::1", nil, true},
		{"2001:db8::/32", nil, true},
		{"1.2.3.4/33", nil, true},
	}
	for _, tc := range cases {
		got, err := ParseIPNets(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%q: len got=%d want=%d", tc.in, len(got), len(tc.want))
		}
		for i := range got {
			if got[i].String() != tc.want[i] {
				// Normalize via ParseCIDR for comparison
				_, wantNet, _ := net.ParseCIDR(tc.want[i])
				if got[i].String() != wantNet.String() {
					t.Fatalf("%q[%d]=%s want %s", tc.in, i, got[i].String(), wantNet.String())
				}
			}
		}
	}
}
