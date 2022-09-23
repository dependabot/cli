package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dependabot/cli/internal/model"
)

type credServer struct {
	server *http.Server
	data   *model.Input
}

// the server receives one payload and shuts itself down
func (s *credServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := json.NewDecoder(r.Body).Decode(&s.data); err != nil {
		panic(err)
	}
	w.WriteHeader(200)
	_ = r.Body.Close()
	go func() {
		_ = s.server.Shutdown(context.Background())
	}()
}

// Input receives configuration via HTTP on the port and returns it decoded
func Input(port int) *model.Input {
	server := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", port),
		ReadHeaderTimeout: time.Second,
	}
	s := &credServer{server: server}
	server.Handler = s
	// printing so the user doesn't think the cli is hanging
	log.Println("waiting for input on port", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
	return s.data
}
