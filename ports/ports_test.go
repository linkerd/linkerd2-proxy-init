package ports

import (
	"strings"
	"testing"
)

func TestParsePort(t *testing.T) {
	tests := []struct {
		input  string
		expect int
	}{
		{"0", 0},
		{"8080", 8080},
		{"65535", 65535},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if check, _ := ParsePort(tt.input); check != tt.expect {
				t.Fatalf("expected %d but received %d", tt.expect, check)
			}
		})
	}
}

func TestParsePort_Errors(t *testing.T) {
	tests := []string{"-1", "65536"}
	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			if r, err := ParsePort(tt); err == nil {
				t.Fatalf("expected error but received %d", r)
			}
		})
	}
}

func TestValidateRange(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"23-23"},
		{"25-27"},
		{"0-65535"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParsePortRange(tt.input)
			assertNoError(t, err)
		})
	}
}

func TestValidateRange_Errors(t *testing.T) {
	tests := []struct {
		input string
		check string
	}{
		{"", "ranges expected as"},
		{"23", "ranges expected as"},
		{"notanumber", "ranges expected as"},
		{"not-number", "not a valid lower-bound"},
		{"-23-25", "ranges expected as"},
		{"-23", "not a valid lower-bound"},
		{"25-23", "upper-bound must be greater than or equal to"},
		{"65536-65539", "not a valid lower-bound"},
		{"23-notanumber", "not a valid upper-bound"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParsePortRange(tt.input)
			assertError(t, err, tt.check)
		})
	}
}

func assertNoError(t *testing.T, err error) {
	if err != nil {
		t.Fatal("expected no error; got", err)
	}
}

// assertError confirms that the provided is an error having the provided message.
func assertError(t *testing.T, err error, containing string) {
	if err == nil {
		t.Fatal("expected error; got nothing")
	}
	if !strings.Contains(err.Error(), containing) {
		t.Fatalf("expected error to contain '%s' but received '%s'", containing, err.Error())
	}
}
