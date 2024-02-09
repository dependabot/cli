package infra

import (
	"testing"
)

func Test_getLatestDigest(t *testing.T) {
	tests := []struct {
		name       string
		imageName  string
		wantDigest string
		wantErr    bool
	}{
		{
			name:       "Test case 1: Valid image name",
			imageName:  "nginx",
			wantDigest: "expectedDigest",
			wantErr:    false,
		},
		{
			name:       "Test case 2: Invalid image name",
			imageName:  "invalid/image",
			wantDigest: "",
			wantErr:    true,
		},
		// Add more test cases as needed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDigest, err := getLatestDigest(tt.imageName)
			if (err != nil) != tt.wantErr {
				t.Errorf("getLatestDigest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotDigest != tt.wantDigest {
				t.Errorf("getLatestDigest() = %v, want %v", gotDigest, tt.wantDigest)
			}
		})
	}
}
