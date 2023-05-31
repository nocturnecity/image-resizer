package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"giggster.com/resizer/pkg"
	"io"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	port   int
	logger *StdLog
	server *http.Server
}

func (s *Server) Run() {
	mux := http.NewServeMux()

	// Register a handler function
	mux.HandleFunc("/resize", s.resizeHandler)

	// Create a new HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,              // set router
		ReadTimeout:  30 * time.Second, // set read timeout
		WriteTimeout: 30 * time.Second, // set write timeout
	}

	s.server = server

	go func() {
		s.logger.Info("ListenAndServe() on port: %d", s.port)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Fatal("ListenAndServe(): %v", err)
		}
	}()
}

func (s *Server) Stop(ctx context.Context) {
	if err := s.server.Shutdown(ctx); err != nil {
		s.logger.Error("HTTP server Shutdown: %v", err)
	}
}

func (s *Server) resizeHandler(w http.ResponseWriter, r *http.Request) {
	if !s.isValidRequest(w, r) {
		return
	}
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		s.processHttpError(w, fmt.Errorf("error reading request body: %w", err), http.StatusBadRequest)
		return
	}
	var req pkg.Request
	err = json.Unmarshal(reqBody, &req)
	if err != nil {
		s.processHttpError(w, fmt.Errorf("error unmarshal request: %w", err), http.StatusBadRequest)
		return
	}
	handler := NewResizeHandler(req, s.logger)
	err = req.Validate()
	if err != nil {
		s.processHttpError(w, fmt.Errorf("validation error: %w", err), http.StatusBadRequest)
		return
	}

	res, err := handler.ProcessRequest()
	if err != nil {
		go handler.CleanupOnError()
		s.processHttpError(w, fmt.Errorf("failed to process image: %w", err), http.StatusInternalServerError)
		return
	}

	s.processHttpSuccess(w, res)
}

func (s *Server) isValidRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "POST" {
		s.processHttpError(w, fmt.Errorf("invalid http method: %s", r.Method), http.StatusNotFound)
		return false
	}

	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		s.processHttpError(w, fmt.Errorf("invalid content type: %s", contentType), http.StatusUnsupportedMediaType)
		return false
	}

	return true
}

func (s *Server) processHttpError(w http.ResponseWriter, err error, status int) {
	s.logger.Error(err.Error())
	response := pkg.ErrorResponse{
		Error: err.Error(),
	}
	if status > 500 {
		response.Error = "Internal Server error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) processHttpSuccess(w http.ResponseWriter, sizes map[string]pkg.ResultSize) {
	response := pkg.Response{
		Sizes: sizes,
	}
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		s.processHttpError(w, err, http.StatusInternalServerError)
		return
	}
}

func NewHttpServer(port int, logger *StdLog) *Server {
	return &Server{
		port:   port,
		logger: logger,
	}
}
