package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"ha-command-gateway/internal/core"
	"ha-command-gateway/internal/logx"
)

// Server est le service HTTP.
type Server struct {
	mux  *http.ServeMux
	port int
}

// New crée le serveur et enregistre les contrôleurs disponibles
func New(port int, apiKey string, sender core.SMSSender) *Server {
	s := &Server{mux: http.NewServeMux(), port: port}
	if sender != nil {
		smsSvc := NewSMSService(sender)
		s.register(NewSMSController(smsSvc, apiKey))
	}
	return s
}

func (s *Server) register(ctrl interface{ Register(*http.ServeMux) }) {
	ctrl.Register(s.mux)
}

func (s *Server) Nom() string { return "api" }

// Démarrer lance le serveur
func (s *Server) Démarrer(ctx context.Context) error {
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: s.mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logx.InfoT("api.demarree", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
