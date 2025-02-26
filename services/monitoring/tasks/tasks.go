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
	Group       string
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
	ts.logger.Info(fmt.Sprintf("Added task %s to scheduler", id))
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

	ts.logger.Info(fmt.Sprintf("Running task %s", id))
	go func() {
		if err := task.Fn(ts.ctx); err != nil {
			ts.logger.Error(fmt.Sprintf("Task %s failed: %v", task.Name, err))
			task.ErrorChan <- err
		}
		task.LastRun = time.Now()
	}()

	return nil
}

// RunAfterAndRemove schedules a task to run after a specific duration and then removes it from the scheduler
func (ts *TaskScheduler) RunAfterAndRemove(id string, duration time.Duration) error {
	ts.mu.Lock()
	task, exists := ts.tasks[id]
	if !exists {
		ts.mu.Unlock()
		return fmt.Errorf("task with ID %s not found", id)
	}

	// Create a copy of task properties under lock
	taskCopy := *task
	ts.mu.Unlock()

	ts.logger.Info(fmt.Sprintf("Scheduling task %s to run after %s and then be removed", id, duration))

	go func() {
		timer := time.NewTimer(duration)
		defer timer.Stop()

		select {
		case <-timer.C:
			// Execute the task
			if err := taskCopy.Fn(ts.ctx); err != nil {
				ts.logger.Error(fmt.Sprintf("Task %s failed: %v", taskCopy.Name, err))

				// Non-blocking send to error channel
				select {
				case taskCopy.ErrorChan <- err:
					// Sent successfully
				default:
					// Channel is full or no one is listening
					ts.logger.Warn(fmt.Sprintf("Could not send error to channel for task %s", id))
				}
			}

			// Update LastRun under lock before removing
			ts.mu.Lock()
			if task, stillExists := ts.tasks[id]; stillExists {
				task.LastRun = time.Now()

				// Remove the task from the scheduler
				delete(ts.tasks, id)
				ts.logger.Info(fmt.Sprintf("Task %s executed and removed from scheduler", id))
			}
			ts.mu.Unlock()

		case <-ts.ctx.Done():
			ts.logger.Info(fmt.Sprintf("Task %s canceled before execution", id))
			return
		}
	}()

	return nil
}

func (ts *TaskScheduler) RunAfter(id string, duration time.Duration) error {
	ts.mu.RLock()
	task, exists := ts.tasks[id]
	ts.mu.RUnlock()

	if !exists {
		return fmt.Errorf("task with ID %s not found", id)
	}

	ts.logger.Info(fmt.Sprintf("Scheduling task %s to run after %s", id, duration))

	go func() {
		timer := time.NewTimer(duration)
		defer timer.Stop()

		<-timer.C

		if err := task.Fn(ts.ctx); err != nil {
			ts.logger.Error(fmt.Sprintf("Task %s failed: %v", task.Name, err))
			task.ErrorChan <- err
		}
		task.LastRun = time.Now()
	}()

	return nil
}

func (ts *TaskScheduler) RunAt(id string, t time.Time) error {
	ts.mu.Lock()
	task, exists := ts.tasks[id]
	if !exists {
		ts.mu.Unlock()
		return fmt.Errorf("task with ID %s not found", id)
	}

	// Create a copy or update task properties under lock
	taskCopy := *task // Copy to avoid race conditions
	ts.mu.Unlock()

	ts.logger.Info(fmt.Sprintf("Scheduling task %s to run at %s", id, t))

	go func() {
		duration := time.Until(t)
		if duration < 0 {
			duration = 0
		}

		timer := time.NewTimer(duration)
		defer timer.Stop()

		select {
		case <-timer.C:
			if err := taskCopy.Fn(ts.ctx); err != nil {
				ts.logger.Error(fmt.Sprintf("Task %s failed: %v", taskCopy.Name, err))

				// Non-blocking send to error channel
				select {
				case taskCopy.ErrorChan <- err:
					// Sent successfully
				default:
					// Channel is full or no one is listening
					ts.logger.Warn(fmt.Sprintf("Could not send error to channel for task %s", id))
				}
			}

			// Update LastRun under lock
			ts.mu.Lock()
			if task, stillExists := ts.tasks[id]; stillExists {
				task.LastRun = time.Now()
			}
			ts.mu.Unlock()

		case <-ts.ctx.Done():
			ts.logger.Info(fmt.Sprintf("Task %s canceled before execution", id))
			return
		}
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

	ts.logger.Info(fmt.Sprintf("Scheduling task %s to run in %s", id, delay))

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		for {
			select {
			case <-ts.ctx.Done():
				ts.logger.Info(fmt.Sprintf("Task %s context cancelled", id))
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
	ts.logger.Info(fmt.Sprintf("Stopped task %s", id))
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
	ts.logger.Info(fmt.Sprintf("Removed task %s from scheduler", id))
	return nil
}

// GetTask retrieves a task by ID
func (ts *TaskScheduler) GetTask(id string) (*Task, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	task, exists := ts.tasks[id]
	if !exists {
		ts.logger.Error(fmt.Sprintf("Task with ID %s not found", id))
		return nil, fmt.Errorf("task with ID %s not found", id)
	}

	return task, nil
}

func (ts *TaskScheduler) GetTaskByName(name string) (*Task, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	for _, task := range ts.tasks {
		if task.Name == name {
			return task, nil
		}
	}

	return nil, fmt.Errorf("task with name %s not found", name)
}

// ListTasks returns all registered tasks
func (ts *TaskScheduler) ListTasks() map[string]*Task {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	tasks := make(map[string]*Task)
	for id, task := range ts.tasks {
		tasks[id] = task
	}
	ts.logger.Info(fmt.Sprintf("Listing tasks: %v", tasks))
	return tasks
}
