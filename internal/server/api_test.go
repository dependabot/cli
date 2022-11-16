package server

import (
	"testing"
)

func Test_decodeWrapper(t *testing.T) {
	t.Run("reject extra data", func(t *testing.T) {
		_, err := decodeWrapper("update_dependency_list", []byte(`data: {"unknown": "value"}`))
		if err == nil {
			t.Error("expected decode would error on extra data")
		}
	})
}
