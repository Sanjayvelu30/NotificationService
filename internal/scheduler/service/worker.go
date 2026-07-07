package service

import (
	"bytes"
	"context"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/sanjay/NotificationService/internal/scheduler/domain"
)

// Worker polls the task execution channel, dispatches callbacks, and schedules retries.
func (s *TaskScheduler) Worker(ctx context.Context, id int) {
	log.Printf("[Worker #%d] Ready to process tasks.", id)
	client := &http.Client{Timeout: 10 * time.Second}

	for t := range s.taskChannel {
		log.Printf("[Worker #%d] Dequeued task %s for processing.", id, t.ID)

		// 1. Update task state to PROCESSING
		_, err := s.repo.Update(t.ID, func(task *domain.Task) {
			task.Status = domain.StatusProcessing
		})
		if err != nil {
			log.Printf("[Worker #%d] Error marking task %s as processing: %v. Skipping.", id, t.ID, err)
			continue
		}

		// 2. Dispatch HTTP callback
		success := s.executeCallback(client, t)

		if success {
			// 3. Mark task COMPLETED
			_, err = s.repo.Update(t.ID, func(task *domain.Task) {
				task.Status = domain.StatusCompleted
			})
			if err != nil {
				log.Printf("[Worker #%d] Error updating task %s status to COMPLETED: %v", id, t.ID, err)
			} else {
				log.Printf("[Worker #%d] Successfully completed execution of task %s.", id, t.ID)
			}
		} else {
			// 4. Handle Failure: increment retry count and determine retry/DLQ status
			s.handleFailure(t)
		}
	}
	log.Printf("[Worker #%d] Exiting worker loop.", id)
}

func (s *TaskScheduler) executeCallback(client *http.Client, t *domain.Task) bool {
	if t.CallbackURL == "" {
		log.Printf("[Worker] Skip empty callback url for task %s.", t.ID)
		return true
	}

	req, err := http.NewRequest("POST", t.CallbackURL, bytes.NewBuffer([]byte(t.Payload)))
	if err != nil {
		log.Printf("[Worker] Failed to create HTTP request for task %s: %v", t.ID, err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	log.Printf("[Worker] Sending POST callback to %s for task %s...", t.CallbackURL, t.ID)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Worker] Callback request failed for task %s: %v", t.ID, err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[Worker] Callback returned non-2xx status code %d for task %s.", resp.StatusCode, t.ID)
		return false
	}

	return true
}

func (s *TaskScheduler) handleFailure(t *domain.Task) {
	_, err := s.repo.Update(t.ID, func(task *domain.Task) {
		task.RetryCount++
		if task.RetryCount > task.MaxRetries {
			task.Status = domain.StatusFailed
		} else {
			task.Status = domain.StatusPending
			// Exponential Backoff: 2^retry_count * 2 seconds (e.g. 1st retry: 4s, 2nd: 8s, 3rd: 16s)
			backoffSec := int(math.Pow(2, float64(task.RetryCount))) * 2
			task.ExecuteAt = time.Now().Add(time.Duration(backoffSec) * time.Second)
		}
	})
	if err != nil {
		log.Printf("[Worker] Error updating failure state for task %s: %v", t.ID, err)
		return
	}

	// Fetch updated task to inspect status
	updatedTask, err := s.repo.Get(t.ID)
	if err != nil {
		log.Printf("[Worker] Failed to retrieve failure record for task %s: %v", t.ID, err)
		return
	}

	if updatedTask.Status == domain.StatusFailed {
		log.Printf("[Worker] Task %s reached maximum retries (%d). Archiving to Dead Letter Queue (DLQ).", t.ID, t.MaxRetries)
		if dlqErr := s.dlqRepo.Save(updatedTask); dlqErr != nil {
			log.Printf("[Worker] Error saving task %s to DLQ database table: %v", t.ID, dlqErr)
		}
	} else {
		log.Printf("[Worker] Task %s execution failed. Scheduled next retry in the database for %v.", t.ID, updatedTask.ExecuteAt.Format(time.RFC3339))
	}
}
