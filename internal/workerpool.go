package internal

import (
	"sync"

	"github.com/nocturnecity/image-resizer/pkg"
)

type job struct {
	h *ResizeHandler
	c chan jobResult
}

type jobResult struct {
	result map[string]pkg.ResultSize
	err    error
}

func NewPool(logger *StdLog, maxWorkers int) *Pool {
	return &Pool{
		logger:  logger,
		wq:      make(chan chan job, maxWorkers),
		workers: maxWorkers,
		qq:      make(chan struct{}),
		wg:      &sync.WaitGroup{},
	}
}

type Pool struct {
	logger  *StdLog
	wq      chan chan job
	qq      chan struct{}
	workers int
	wg      *sync.WaitGroup
}

func (d *Pool) Run() {
	d.logger.Info("Starting worker pool with %d workers", d.workers)
	for i := 0; i < d.workers; i++ {
		d.wg.Add(1)
		worker := newWorker(d.logger, d.wq, d.qq, d.wg)
		worker.start()
	}
}

func (d *Pool) Dispatch(j job) {
	jobChannel := <-d.wq
	jobChannel <- j
}

func (d *Pool) ShutDown() {
	close(d.qq)
	d.wg.Wait()
}

func newWorker(logger *StdLog, workerQueue chan chan job, quitChan chan struct{}, wg *sync.WaitGroup) *Worker {
	return &Worker{
		logger: logger,
		jq:     make(chan job),
		wq:     workerQueue,
		qc:     quitChan,
		wg:     wg,
	}
}

type Worker struct {
	logger *StdLog
	jq     chan job      // internal worker queue
	wq     chan chan job // pool workers queue
	qc     chan struct{}
	wg     *sync.WaitGroup
}

func (w *Worker) start() {
	go func() {
		w.logger.Info("Worker spawned")
		defer func() {
			if r := recover(); r != nil {
				w.logger.Error("worker panic recover: %w", r)
				w.start()
			}
		}()
		for {
			w.wq <- w.jq
			select {
			case rq := <-w.jq:
				w.logger.Debug("Worker processing request %v", rq.h.Request)
				result, err := rq.h.ProcessRequest()
				rq.c <- jobResult{
					result,
					err,
				}
			case <-w.qc:
				w.logger.Debug("Worker quit channel triggered")
				w.wg.Done()
				return
			}
		}
	}()
}
