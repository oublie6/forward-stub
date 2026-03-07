package config

import "testing"

func TestValidateSSHHostKeyFingerprint(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		valid bool
	}{
		{name: "valid", in: "SHA256:W5M5Qf3jQ8jD8I2LqzY9zT6QfPj1O9g3k8xw0Jm9r3A", valid: true},
		{name: "empty", in: "", valid: false},
		{name: "bad prefix", in: "MD5:abc", valid: false},
		{name: "missing digest", in: "SHA256:", valid: false},
		{name: "invalid base64", in: "SHA256:***", valid: false},
	}
	for _, tc := range cases {
		err := ValidateSSHHostKeyFingerprint(tc.in)
		if tc.valid && err != nil {
			t.Fatalf("%s: expected valid, got %v", tc.name, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("%s: expected invalid", tc.name)
		}
	}
}
