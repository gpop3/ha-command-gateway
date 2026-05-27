package api

import (
	"fmt"
	"log"
	"net/http"
)

// Server représente le serveur HTTP de l'assistant
type Server struct {
	mux  *http.ServeMux
	port int
}

// NewServer crée un nouveau serveur HTTP
func NewServer(port int) *Server {
	return &Server{
		mux:  http.NewServeMux(),
		port: port,
	}
}

// Register enregistre un contrôleur sur le serveur
func (s *Server) Register(ctrl interface{ Register(*http.ServeMux) }) {
	ctrl.Register(s.mux)
}

// Start démarre le serveur HTTP en arrière-plan
func (s *Server) Start() {
	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("🌐 API HTTP démarrée sur %s", addr)
	go func() {
		if err := http.ListenAndServe(addr, s.mux); err != nil {
			log.Fatalf("❌ Erreur serveur HTTP : %v", err)
		}
	}()
}
