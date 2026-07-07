package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/sanjay/NotificationService/internal/scheduler/domain"
	"github.com/sanjay/NotificationService/internal/scheduler/repository"
)

type TaskScheduler struct {
	repo          repository.TaskRepo
	dlqRepo       repository.DLQRepo
	workerCount   int
	queueCapacity int
	taskChannel   chan *domain.Task
	wg            sync.WaitGroup
	cancel        context.CancelFunc
}

// NewTaskScheduler creates a new background TaskScheduler.
func NewTaskScheduler(repo repository.TaskRepo, dlqRepo repository.DLQRepo, workerCount, queueCapacity int) *TaskScheduler {
	if workerCount <= 0 {
		workerCount = 5
	}
	if queueCapacity <= 0 {
		queueCapacity = 10
	}

	return &TaskScheduler{
		repo:          repo,
		dlqRepo:       dlqRepo,
		workerCount:   workerCount,
		queueCapacity: queueCapacity,
		taskChannel:   make(chan *domain.Task, queueCapacity),
	}
}

// Start boots up the background workers and begins the ticker loop.
func (s *TaskScheduler) Start(ctx context.Context) {
	// 1. Startup Recovery: Revert any tasks stuck in QUEUED or PROCESSING back to PENDING.
	if err := s.repo.RecoverTasks(ctx); err != nil {
		log.Printf("[Scheduler] WARNING: Failed to recover stuck tasks on startup: %v", err)
	}

	// 2. Start the worker pool
	for i := 0; i < s.workerCount; i++ {
		s.wg.Add(1)
		go func(workerID int) {
			defer s.wg.Done()
			s.Worker(ctx, workerID)
		}(i + 1)
	}
	log.Printf("[Scheduler] Started worker pool with %d concurrent workers.", s.workerCount)

	// 3. Start ticking scheduler loop
	ctx, s.cancel = context.WithCancel(ctx)
	go s.tickLoop(ctx)
}

// Stop gracefully halts the scheduler and drains the execution queue.
func (s *TaskScheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	close(s.taskChannel)
	s.wg.Wait()
	log.Println("[Scheduler] Stopped scheduler and shut down all background workers.")
}

func (s *TaskScheduler) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	log.Println("[Scheduler] Ticker loop started. Ticking once per second.")

	for {
		select {
		case <-ctx.Done():
			log.Println("[Scheduler] Ticker loop received cancellation signal. Exiting.")
			return
		case <-ticker.C:
			// Backpressure check: restrict database extraction to match queue buffer availability
			remainingCap := cap(s.taskChannel) - len(s.taskChannel)
			if remainingCap <= 0 {
				log.Println("[Scheduler] Warning: Worker queue channel is full. Skipping database claiming tick.")
				continue
			}

			// Atomic fetch of ready tasks using Postgres FOR UPDATE SKIP LOCKED
			tasks, err := s.repo.ClaimReadyTasks(time.Now(), remainingCap)
			if err != nil {
				log.Printf("[Scheduler] Error claiming ready tasks from database: %v", err)
				continue
			}

			for _, t := range tasks {
				select {
				case s.taskChannel <- t:
					log.Printf("[Scheduler] Enqueued task %s matching execution schedule.", t.ID)
				default:
					log.Printf("[Scheduler] Error: Task channel saturated. Bypassed task %s.", t.ID)
				}
			}
		}
	}
}
