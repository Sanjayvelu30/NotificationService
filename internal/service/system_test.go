package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sanjay/NotificationService/internal/domain"
)

type mockNotificationRepo struct {
	mu           sync.Mutex
	data         map[string]domain.Notification
	saveCalled   int
	updateCalled int
}

func (m *mockNotificationRepo) GetTotalCount() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.data), nil
}

type mockTemplateRepo struct {
	mu   sync.Mutex
	data map[string]string
}

func (m *mockTemplateRepo) Save(name string, body string, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[name] = body
	return nil
}

func (m *mockTemplateRepo) Get(name string, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, exists := m.data[name]
	if !exists {
		return "", errors.New("template not found")
	}
	return body, nil
}

func (m *mockTemplateRepo) GetAll(userID string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := make(map[string]string)
	for k, v := range m.data {
		res[k] = v
	}
	return res, nil
}

func (m *mockNotificationRepo) Save(n domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[n.ID] = n
	m.saveCalled++
	return nil
}

func (m *mockNotificationRepo) Update(n domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[n.ID] = n
	m.updateCalled++
	return nil
}

func (m *mockNotificationRepo) Get(id string, userID string) (domain.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, exists := m.data[id]
	if !exists {
		return domain.Notification{}, errors.New("not found")
	}
	return n, nil
}

func (m *mockNotificationRepo) GetReadyJobs() ([]domain.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ready []domain.Notification
	for _, n := range m.data {
		if n.Status == domain.Pending {
			ready = append(ready, n)
		}
	}
	return ready, nil
}

type mockDLQRepo struct {
	mu   sync.Mutex
	data map[string]domain.Notification
}

func (m *mockDLQRepo) Save(n domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[n.ID] = n
	return nil
}

func (m *mockDLQRepo) Get(id string, userID string) (domain.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, exists := m.data[id]
	if !exists {
		return domain.Notification{}, errors.New("not found")
	}
	return n, nil
}

type mockSender struct {
	err error
}

func (m *mockSender) Send(n domain.Notification) error {
	return m.err
}

func TestProcess_Success(t *testing.T) {
	repo := &mockNotificationRepo{data: make(map[string]domain.Notification)}
	dlq := &mockDLQRepo{data: make(map[string]domain.Notification)}
	sender := &mockSender{err: nil}
	tmplRepo := &mockTemplateRepo{data: map[string]string{"WELCOME": "Welcome to our service, {{name}}!"}}

	sys := &NotificationSystem{
		MaxRetryCount:    3,
		NotificationRepo: repo,
		TemplateRepo:     tmplRepo,
		DLQRepo:          dlq,
		NotificationStrategy: map[domain.NotificationType]NotificationSender{
			domain.Email: sender,
		},
	}

	n := domain.Notification{
		ID:        "test-id-1",
		Recipient: "john@example.com",
		Template:  "WELCOME",
		Type:      domain.Email,
		Status:    domain.Pending,
	}

	sys.Process(n)

	saved, err := repo.Get("test-id-1", "")
	if err != nil {
		t.Fatalf("expected saved notification, got error: %v", err)
	}

	if saved.Status != domain.Sent {
		t.Errorf("expected status SENT, got %s", saved.Status)
	}
}

func TestProcess_RetryAndDLQ(t *testing.T) {
	repo := &mockNotificationRepo{data: make(map[string]domain.Notification)}
	dlq := &mockDLQRepo{data: make(map[string]domain.Notification)}
	sender := &mockSender{err: errors.New("delivery failed")}
	tmplRepo := &mockTemplateRepo{data: map[string]string{"WELCOME": "Welcome to our service, {{name}}!"}}

	sys := &NotificationSystem{
		MaxRetryCount:    3,
		NotificationRepo: repo,
		TemplateRepo:     tmplRepo,
		DLQRepo:          dlq,
		NotificationStrategy: map[domain.NotificationType]NotificationSender{
			domain.Email: sender,
		},
	}

	n := domain.Notification{
		ID:         "test-id-2",
		Recipient:  "john@example.com",
		Template:   "WELCOME",
		Type:       domain.Email,
		Status:     domain.Pending,
		RetryCount: 0,
	}

	// First failure - should increment RetryCount and set status back to Pending
	sys.Process(n)

	saved, err := repo.Get("test-id-2", "")
	if err != nil {
		t.Fatalf("expected saved notification, got error: %v", err)
	}

	if saved.Status != domain.Pending {
		t.Errorf("expected status PENDING for retry, got %s", saved.Status)
	}
	if saved.RetryCount != 1 {
		t.Errorf("expected retry count 1, got %d", saved.RetryCount)
	}
	if saved.NextRetryAt.Before(time.Now()) {
		t.Errorf("expected NextRetryAt to be in the future")
	}

	// Fast forward: set retry count to 3 (equal to MaxRetryCount) and fail again
	saved.RetryCount = 3
	sys.Process(saved)

	savedAfterDLQ, err := repo.Get("test-id-2", "")
	if err != nil {
		t.Fatalf("expected saved notification, got error: %v", err)
	}

	if savedAfterDLQ.Status != domain.DLQ {
		t.Errorf("expected status DLQ after exceeding max retries, got %s", savedAfterDLQ.Status)
	}

	// Ensure it is in the DLQ repo
	dlqItem, err := dlq.Get("test-id-2", "")
	if err != nil {
		t.Fatalf("expected item in DLQ repository, got: %v", err)
	}
	if dlqItem.Status != domain.DLQ {
		t.Errorf("expected DLQ status in DLQ repo, got %s", dlqItem.Status)
	}
}
