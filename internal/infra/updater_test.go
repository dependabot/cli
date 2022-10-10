package infra

import (
	"os"
	"path/filepath"
	"testing"
)

func Test_mountOptions(t *testing.T) {
	wd, _ := os.Getwd()
	tests := []struct {
		input            string
		expectedLocal    string
		expectedRemote   string
		expectedReadOnly bool
		expectedErr      error
	}{
		{
			input:       "",
			expectedErr: ErrInvalidVolume,
		}, {
			input:          "local:remote",
			expectedLocal:  filepath.Join(wd, "local"),
			expectedRemote: "remote",
		}, {
			input:            "local:remote:ro",
			expectedLocal:    filepath.Join(wd, "local"),
			expectedRemote:   "remote",
			expectedReadOnly: true,
		}, {
			input:            ".:remote:ro",
			expectedLocal:    wd,
			expectedRemote:   "remote",
			expectedReadOnly: true,
		}, {
			input:       "local:remote:ro:hi",
			expectedErr: ErrInvalidVolume,
		}, {
			input:       "local:remote:wo",
			expectedErr: ErrInvalidVolume,
		},
	}

	for _, test := range tests {
		local, remote, readOnly, err := mountOptions(test.input)
		if local != test.expectedLocal || remote != test.expectedRemote || readOnly != test.expectedReadOnly || err != test.expectedErr {
			t.Errorf("For input '%v' got '%v' '%v' '%v' '%v'", test.input, local, remote, readOnly, err)
		}
	}
}
