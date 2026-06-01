package hooks

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/live"
	"github.com/cursor-stat/cursor-stat/internal/store"
)

const DefaultPort = 23556

// Server receives live hook events on loopback.
type Server struct {
	Port int
	ring *live.Ring
	db   *store.DB
	mu   sync.Mutex
	srv  *http.Server
}

// NewServer creates a hook HTTP server.
func NewServer(ring *live.Ring, db *store.DB, port int) *Server {
	if port <= 0 {
		port = DefaultPort
	}
	return &Server{Port: port, ring: ring, db: db}
}

// Start listens on 127.0.0.1:port until ctx done.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/event", s.handleEvent)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	s.mu.Lock()
	addr := fmt.Sprintf("127.0.0.1:%d", s.Port)
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.mu.Unlock()

	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		_ = s.srv.Close()
	}()

	err = s.srv.Serve(ln)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}

	at := time.Now().UTC()
	ev, choice := ParseEvent(body)
	ev.At = at
	if ev.Model != "" {
		ev.Detail = cursor.NormalizeModel(ev.Model)
		if ev.Manual {
			ev.Detail += " (manual)"
		}
	}

	if s.ring != nil {
		s.ring.Push(ev)
	}
	if choice != nil && s.db != nil {
		choice.At = at
		_, _ = s.db.InsertModelChoice(*choice)
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}
