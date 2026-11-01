package core

import (
	"errors"
	"testing"

	"github.com/tivjnb/futi/internal/argvtmpl"
)

func TestBatchCheck(t *testing.T) {
	// The lookup finds only argv0: a check that looks up another word (an
	// argument, the module name) gets "not found" and the test fails.
	found := func(name string) (string, error) {
		if name != "deploy" {
			return "", errors.New("looked up " + name + ", want deploy")
		}
		return `C:/x/deploy.cmd`, nil
	}
	missing := func(string) (string, error) { return "", errors.New("not found") }
	unsafe := []argvtmpl.Arg{{Value: "a&b", Users: []string{"name"}}}
	literal := []argvtmpl.Arg{{Value: "a&b"}}

	tests := []struct {
		name string
		goos string
		look func(string) (string, error)
		args []argvtmpl.Arg
		want string
	}{
		{"windows unsafe value", "windows", found, unsafe, `value for <name> contains "&", unsafe for batch file deploy.cmd`},
		{"windows literal element", "windows", found, literal, ""},
		{"linux skips check", "linux", found, unsafe, ""},
		{"lookup failure is left to the executor", "windows", missing, unsafe, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := batchCheck(tc.goos, tc.look, "deploy", tc.args)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("batchCheck = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("batchCheck = %v, want %s", err, tc.want)
			}
		})
	}
}
