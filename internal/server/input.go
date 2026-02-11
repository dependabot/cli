package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
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
func Input(listener net.Listener) (*model.Input, error) {
	handler := &credServer{}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	handler.server = srv

	// printing so the user doesn't think the cli is hanging
	log.Println("waiting for input on", listener.Addr())
	if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return nil, err
	}
	return handler.data, nil
}
