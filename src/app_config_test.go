package main

import (
	"testing"

	logging "github.com/ipfs/go-log/v2"
)

func TestTolerateSokMismatchEnvParsing(t *testing.T) {
	log := logging.Logger("test")

	testCases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "unset defaults to off", value: "", want: false},
		{name: "explicitly disabled", value: "0", want: false},
		{name: "enabled", value: "1", want: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TOLERATE_SOK_MISMATCH", tc.value)
			if got := boolEnvChecked("TOLERATE_SOK_MISMATCH", log); got != tc.want {
				t.Errorf("boolEnvChecked(TOLERATE_SOK_MISMATCH=%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
