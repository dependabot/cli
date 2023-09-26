package server

import (
	"net/http"
	"net/http/httptest"
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

func TestAPI_ServeHTTP(t *testing.T) {
	t.Run("doesn't crash when unknown endpoint is used", func(t *testing.T) {
		request := httptest.NewRequest("POST", "/unexpected-endpoint", nil)
		response := httptest.NewRecorder()

		api := NewAPI(nil, nil)
		api.ServeHTTP(response, request)

		if response.Code != http.StatusNotImplemented {
			t.Errorf("expected status code %d, got %d", http.StatusNotImplemented, response.Code)
		}
	})
}
