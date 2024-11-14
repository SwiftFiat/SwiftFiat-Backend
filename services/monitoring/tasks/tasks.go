package tasks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SwiftFiat/SwiftFiat-Backend/services/monitoring/logging"
)

// Task represents a scheduled task
type Task struct {
	ID          string
	Name        string
	Fn          func(context.Context) error
	Interval    time.Duration // For recurring tasks. Zero means run once
	LastRun     time.Time
	IsRecurring bool
	ErrorChan   chan error // Channel to send execution errors
}

// TaskScheduler manages all scheduled tasks
type TaskScheduler struct {
	tasks  map[string]*Task
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	logger *logging.Logger // Using your existing logger
}

// NewTaskScheduler creates a new TaskScheduler
func NewTaskScheduler(logger *logging.Logger) *TaskScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &TaskScheduler{
		tasks:  make(map[string]*Task),
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}
}

// AddTask adds a new task to the scheduler
func (ts *TaskScheduler) AddTask(id, name string, fn func(context.Context) error, interval time.Duration) (*Task, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if _, exists := ts.tasks[id]; exists {
		return nil, fmt.Errorf("task with ID %s already exists", id)
	}

	task := &Task{
		ID:          id,
		Name:        name,
		Fn:          fn,
		Interval:    interval,
		IsRecurring: interval > 0,
		ErrorChan:   make(chan error, 1),
	}

	ts.tasks[id] = task
	return task, nil
}

// RunTask immediately executes a specific task
func (ts *TaskScheduler) RunTask(id string) error {
	ts.mu.RLock()
	task, exists := ts.tasks[id]
	ts.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task with ID %s not found", id)
	}

	go func() {
		if err := task.Fn(ts.ctx); err != nil {
			ts.logger.Error(fmt.Sprintf("Task %s failed: %v", task.Name, err))
			task.ErrorChan <- err
		}
		task.LastRun = time.Now()
	}()

	return nil
}

// ScheduleTask schedules a task to run after a delay
func (ts *TaskScheduler) ScheduleTask(id string, delay time.Duration) error {
	ts.mu.RLock()
	task, exists := ts.tasks[id]
	ts.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task with ID %s not found", id)
	}

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		for {
			select {
			case <-ts.ctx.Done():
				return
			case <-timer.C:
				if err := task.Fn(ts.ctx); err != nil {
					ts.logger.Error(fmt.Sprintf("Task %s failed: %v", task.Name, err))
					task.ErrorChan <- err
				}
				task.LastRun = time.Now()

				if !task.IsRecurring {
					return
				}
				timer.Reset(task.Interval)
			}
		}
	}()

	return nil
}

// StopTask stops a running task
func (ts *TaskScheduler) StopTask(id string) error {
	ts.mu.RLock()
	_, exists := ts.tasks[id]
	ts.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task with ID %s not found", id)
	}

	ts.cancel()
	ts.ctx, ts.cancel = context.WithCancel(context.Background())
	return nil
}

// RemoveTask removes a task from the scheduler
func (ts *TaskScheduler) RemoveTask(id string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if _, exists := ts.tasks[id]; !exists {
		return fmt.Errorf("task with ID %s not found", id)
	}

	delete(ts.tasks, id)
	return nil
}

// GetTask retrieves a task by ID
func (ts *TaskScheduler) GetTask(id string) (*Task, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	task, exists := ts.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task with ID %s not found", id)
	}

	return task, nil
}

// ListTasks returns all registered tasks
func (ts *TaskScheduler) ListTasks() map[string]*Task {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	tasks := make(map[string]*Task)
	for id, task := range ts.tasks {
		tasks[id] = task
	}
	return tasks
}
