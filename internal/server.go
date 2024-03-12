package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nocturnecity/image-resizer/pkg"
)

type Server struct {
	port              int
	watermarkProvider *WatermarkProvider
	logger            *StdLog
	server            *http.Server
	pool              *Pool
	timeout           time.Duration
	resizeMemoryLimit int
	workersCount      int
}

// Define a new Prometheus counter
var resizeRequests = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "resize_requests_total",
		Help: "Total number of resize requests received.",
	},
)

var queueLength = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "queue_length",
		Help: "Current Queue length.",
	},
)

var (
	resizeDurationWithQueueWait = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "resize_duration_with_queue_wait_milliseconds",
		Help:    "The duration of the resize plus waiting in queue in milliseconds",
		Buckets: prometheus.LinearBuckets(1000, 2500, 40), // from 1 to 100 seconds per 2.5 second
	})
)

var (
	resizeDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "resize_duration_milliseconds",
		Help:    "The duration of the resize in milliseconds",
		Buckets: prometheus.LinearBuckets(1000, 2500, 40), // from 1 to 100 seconds per 2.5 second
	})
)

var failedResizes = prometheus.NewCounter(
	prometheus.CounterOpts{
		Name: "resize_failures_total",
		Help: "Total number of failed resize operations.",
	},
)

func (s *Server) Run() {
	mux := http.NewServeMux()

	// Register a handler function
	mux.HandleFunc("/resize", s.resizeHandler)

	mux.HandleFunc("/healthz", s.healthzHandler)

	mux.Handle("/metrics", promhttp.Handler())

	// Create a new HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,       // set router
		ReadTimeout:  s.timeout, // set read timeout
		WriteTimeout: s.timeout, // set write timeout
	}

	s.server = server

	// Register it with Prometheus
	prometheus.MustRegister(queueLength)
	prometheus.MustRegister(resizeRequests)
	prometheus.MustRegister(failedResizes)
	prometheus.MustRegister(resizeDuration)
	prometheus.MustRegister(resizeDurationWithQueueWait)

	s.pool = NewPool(s.logger, s.workersCount)
	s.pool.Run()
	go func() {
		s.logger.Info("ListenAndServe() on port: %d", s.port)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			s.Stop(context.Background())
			s.logger.Fatal("ListenAndServe(): %v", err)
		}
	}()
}

func (s *Server) Stop(ctx context.Context) {
	if err := s.server.Shutdown(ctx); err != nil {
		s.logger.Error("HTTP server Shutdown: %v", err)
	}
	s.pool.ShutDown()
	s.watermarkProvider.ShutDown()
	s.logger.Info("Application stopped")
}

func (s *Server) resizeHandler(w http.ResponseWriter, r *http.Request) {
	resizeRequests.Inc()
	start := time.Now()

	if !s.isValidRequest(w, r) {
		return
	}
	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		failedResizes.Inc()
		s.processHttpError(r, w, fmt.Errorf("error reading request body: %w", err), http.StatusBadRequest)
		return
	}
	var req pkg.Request
	err = json.Unmarshal(reqBody, &req)
	if err != nil {
		failedResizes.Inc()
		s.processHttpError(r, w, fmt.Errorf("error unmarshal request: %w", err), http.StatusBadRequest)
		return
	}
	err = req.Validate()
	if err != nil {
		failedResizes.Inc()
		s.processHttpError(r, w, fmt.Errorf("validation error: %w", err), http.StatusBadRequest)
		return
	}

	handler := NewResizeHandler(req, s.logger, s.watermarkProvider,
		ResizerConfig{MemoryMB: s.resizeMemoryLimit, TimeoutSec: int(s.timeout.Seconds())})
	defer handler.Cleanup()
	resChan := make(chan jobResult)
	queueLength.Inc()
	s.pool.Dispatch(job{
		h: handler,
		c: resChan,
	})
	poolRes := <-resChan
	defer queueLength.Dec()
	res, err := poolRes.result, poolRes.err
	if err != nil {
		go handler.CleanupOnError()
		failedResizes.Inc()
		s.processHttpError(r, w, fmt.Errorf("failed to process image: %w", err), http.StatusInternalServerError)
		return
	}
	durationMs := float64(time.Since(start).Milliseconds())
	resizeDurationWithQueueWait.Observe(durationMs)
	s.logger.Debug("RESIZE OBSERVED EXECUTION TIME FOR %s: %.2f sec", req.OriginalPath, durationMs/1000)
	s.processHttpSuccess(r, w, res)
}

func (s *Server) isValidRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "POST" {
		s.processHttpError(r, w, fmt.Errorf("invalid http method: %s", r.Method), http.StatusNotFound)
		return false
	}

	contentType := r.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") {
		s.processHttpError(r, w, fmt.Errorf("invalid content type: %s", contentType), http.StatusUnsupportedMediaType)
		return false
	}

	return true
}

func (s *Server) processHttpError(r *http.Request, w http.ResponseWriter, err error, status int) {
	s.logger.Error("%s %s error %v", r.Method, r.URL, err.Error())
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

func (s *Server) processHttpSuccess(r *http.Request, w http.ResponseWriter, sizes map[string]pkg.ResultSize) {
	response := pkg.Response{
		Sizes: sizes,
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		s.processHttpError(r, w, err, http.StatusInternalServerError)
		return
	}
	s.logger.Info("%s %s %d", r.Method, r.URL, http.StatusOK)
}

func (s *Server) healthzHandler(w http.ResponseWriter, r *http.Request) {
	// You can add any logic here to check your application's health
	// For simplicity, this handler will always return HTTP 200 OK
	w.WriteHeader(http.StatusOK)
	s.logger.Info("%s %s %d", r.Method, r.URL, http.StatusOK)
}

func NewHttpServer(port int, timeout time.Duration, memoryLimit, workersCount int, logger *StdLog) *Server {
	return &Server{
		port:              port,
		logger:            logger,
		timeout:           timeout,
		resizeMemoryLimit: memoryLimit,
		workersCount:      workersCount,
		watermarkProvider: NewWatermarkProvider(logger),
	}
}
