package internal

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultCacheTTL        time.Duration = time.Hour
	DefaultJanitorInterval time.Duration = time.Minute
)

func NewWatermarkProvider(log *StdLog) *WatermarkProvider {
	return &WatermarkProvider{
		cache: newWatermarkCache(log),
		log:   log,
	}
}

type WatermarkProvider struct {
	cache *watermarkCache
	log   *StdLog
}

func (wp *WatermarkProvider) GetWatermark(url string) (string, string, error) {
	path, format, ok := wp.cache.Get(url)
	if ok {
		return path, format, nil
	}

	path, format, err := wp.downloadWatermarkFile(url)
	if err != nil {
		return "", "", fmt.Errorf("download watermark error: %v", err)
	}
	wp.cache.Set(url, path, format)

	return path, format, nil
}

func (wp *WatermarkProvider) downloadWatermarkFile(url string) (string, string, error) {
	response, err := http.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("failed to make HTTP request: %v", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			wp.log.Error("error closing watermark download request body: %v", err)
		}
	}(response.Body)
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unexpected status code: %d", response.StatusCode)
	}
	watermarkFormat, err := getFileExtensionFromUrl(url)
	if err != nil {
		wp.log.Error("can't identify watermark image format: %w", err)
		watermarkFormat = DefaultJpegFormat
	}
	tempFile, err := os.CreateTemp("", fmt.Sprintf("%s.%s", uuid.New(), watermarkFormat))
	if err != nil {
		return "", "", fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer func(tempFile *os.File) {
		err := tempFile.Close()
		if err != nil {
			wp.log.Error("error closing watermark temporary file: %v", err)
		}
	}(tempFile)
	_, err = io.Copy(tempFile, response.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to copy response body to file: %v", err)
	}

	format := filepath.Ext(tempFile.Name())
	return tempFile.Name(), format, nil
}

func (wp *WatermarkProvider) ShutDown() {
	wp.cache.Shutdown()
}

func newWatermarkCache(l *StdLog) *watermarkCache {
	c := &watermarkCache{
		ttl:      DefaultCacheTTL,
		entities: map[string]watermarkCacheEntity{},
		mu:       sync.RWMutex{},
		l:        l,
	}
	defer runJanitor(c, DefaultJanitorInterval)
	runtime.SetFinalizer(c, stopJanitor)
	return c
}

type watermarkCache struct {
	entities map[string]watermarkCacheEntity
	ttl      time.Duration
	mu       sync.RWMutex
	j        *janitor
	l        *StdLog
}

type watermarkCacheEntity struct {
	path      string
	format    string
	expiredAt int64
}

func (w *watermarkCache) Get(key string) (string, string, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	val, ok := w.entities[key]
	if !ok {
		return "", "", false
	}

	return val.path, val.format, true
}

func (w *watermarkCache) Set(key, path, format string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entities[key] = watermarkCacheEntity{
		path:      path,
		format:    format,
		expiredAt: time.Now().Add(w.ttl).UnixNano(),
	}
}

func (w *watermarkCache) Shutdown() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, v := range w.entities {
		w.clearEntity(k, v.path)
	}
	stopJanitor(w)
}

func (w *watermarkCache) DeleteExpired() {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now().UnixNano()
	for k, v := range w.entities {
		if now > v.expiredAt {
			w.clearEntity(k, v.path)
		}
	}
}

func (w *watermarkCache) clearEntity(key, toDelete string) {
	delete(w.entities, key)
	err := os.Remove(toDelete)
	if err != nil {
		w.l.Error("error clean up file delete: %v", err)
	}
}

type janitor struct {
	Interval time.Duration
	stop     chan bool
}

func (j *janitor) Run(c *watermarkCache) {
	ticker := time.NewTicker(j.Interval)
	for {
		select {
		case <-ticker.C:
			c.DeleteExpired()
		case <-j.stop:
			ticker.Stop()
			return
		}
	}
}

func stopJanitor(c *watermarkCache) {
	c.j.stop <- true
}

func runJanitor(c *watermarkCache, ci time.Duration) {
	j := &janitor{
		Interval: ci,
		stop:     make(chan bool),
	}
	c.j = j
	go j.Run(c)
}

func getFileExtensionFromUrl(rawUrl string) (string, error) {
	u, err := url.Parse(rawUrl)
	if err != nil {
		return "", err
	}
	pos := strings.LastIndex(u.Path, ".")
	if pos == -1 {
		return "", fmt.Errorf("couldn't find a period to indicate a file extension")
	}
	return u.Path[pos+1 : len(u.Path)], nil
}
