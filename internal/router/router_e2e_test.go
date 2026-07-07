package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sanjay/NotificationService/internal/domain"
	"github.com/sanjay/NotificationService/internal/handler"
	"github.com/sanjay/NotificationService/internal/router"
	"github.com/sanjay/NotificationService/internal/service"
)

// In-Memory Repository Mocks
type mockTemplateRepository struct {
	mu        sync.Mutex
	templates map[string]map[string]string // user_id -> template_name -> body
}

func newMockTemplateRepository() *mockTemplateRepository {
	return &mockTemplateRepository{
		templates: make(map[string]map[string]string),
	}
}

func (m *mockTemplateRepository) Save(name string, body string, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.templates[userID]; !ok {
		m.templates[userID] = make(map[string]string)
	}
	m.templates[userID][name] = body
	return nil
}

func (m *mockTemplateRepository) Get(name string, userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Try user specific first
	if t, ok := m.templates[userID]; ok {
		if body, found := t[name]; found {
			return body, nil
		}
	}
	// Try global fallback
	if t, ok := m.templates["global"]; ok {
		if body, found := t[name]; found {
			return body, nil
		}
	}
	return "", fmt.Errorf("template not found: %s", name)
}

func (m *mockTemplateRepository) GetAll(userID string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]string)
	// Pull global defaults
	if t, ok := m.templates["global"]; ok {
		for k, v := range t {
			result[k] = v
		}
	}
	// Override with user specifics
	if t, ok := m.templates[userID]; ok {
		for k, v := range t {
			result[k] = v
		}
	}
	return result, nil
}

type mockNotificationRepository struct {
	mu            sync.Mutex
	notifications map[string]domain.Notification
}

func newMockNotificationRepository() *mockNotificationRepository {
	return &mockNotificationRepository{
		notifications: make(map[string]domain.Notification),
	}
}

func (m *mockNotificationRepository) Save(n domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications[n.ID] = n
	return nil
}

func (m *mockNotificationRepository) Update(n domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications[n.ID] = n
	return nil
}

func (m *mockNotificationRepository) Get(id string, userID string) (domain.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, found := m.notifications[id]
	if !found {
		return domain.Notification{}, fmt.Errorf("notification not found")
	}
	return n, nil
}

func (m *mockNotificationRepository) GetReadyJobs() ([]domain.Notification, error) {
	return []domain.Notification{}, nil
}

func (m *mockNotificationRepository) GetTotalCount() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.notifications), nil
}

type mockDLQRepository struct{}

func (m *mockDLQRepository) Save(n domain.Notification) error { return nil }
func (m *mockDLQRepository) Get(id string, userID string) (domain.Notification, error) {
	return domain.Notification{}, nil
}

type mockIdempotencyRepository struct {
	mu   sync.Mutex
	keys map[string]any
}

func newMockIdempotencyRepository() *mockIdempotencyRepository {
	return &mockIdempotencyRepository{
		keys: make(map[string]any),
	}
}

func (m *mockIdempotencyRepository) Exisits(key string) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.keys[key], nil
}

func (m *mockIdempotencyRepository) Save(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[key] = "PENDING"
	return nil
}

func (m *mockIdempotencyRepository) Update(key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[key] = value
	return nil
}

type mockEmailSender struct{}

func (s *mockEmailSender) Send(n domain.Notification) error { return nil }

func TestE2E_NotificationServiceLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. Initialize Mock Dependencies
	templateRepo := newMockTemplateRepository()
	notificationRepo := newMockNotificationRepository()
	dlqRepo := &mockDLQRepository{}
	idempotencyRepo := newMockIdempotencyRepository()

	// Seed a global template default
	_ = templateRepo.Save("WELCOME", "Welcome {{name}}!", "global")

	// Set up a mock HTTP server to represent the Task Scheduler microservice
	mockSchedulerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"created"}`))
	}))
	defer mockSchedulerServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	system := &service.NotificationSystem{
		WorkerCount:         1,
		MaxRetryCount:       3,
		Ctx:                 ctx,
		Cancel:              cancel,
		NotificationChannel: make(chan domain.Notification, 64),
		NotificationRepo:    notificationRepo,
		TemplateRepo:        templateRepo,
		IdempotencyRepo:     idempotencyRepo,
		DLQRepo:             dlqRepo,
		SchedulerURL:        mockSchedulerServer.URL,
		SchedulerAPIKey:     "test_api_key_12345",
		NotificationStrategy: map[domain.NotificationType]service.NotificationSender{
			domain.Email: &mockEmailSender{},
		},
	}
	system.Start()

	// 2. Wire Mocks into Handlers
	notificationHandler := handler.NewNotificationHandler(system)
	templateHandler := handler.NewTemplateHandler(templateRepo)

	// Mock Authentication Middleware (always injects test_user)
	mockAuth := func(c *gin.Context) {
		c.Set("user_id", "test_user")
		c.Next()
	}

	// Mock Rate Limiting Middleware (always allows requests)
	mockRateLimit := func(c *gin.Context) {
		c.Next()
	}

	r := router.New(notificationHandler, templateHandler, mockRateLimit, mockAuth)

	// ==========================================
	// STEP A: CREATE CUSTOM TEMPLATE
	// ==========================================
	t.Run("Create Custom Template", func(t *testing.T) {
		reqBody := map[string]string{
			"name": "MFA",
			"body": "Your code is {{code}}",
		}
		jsonBytes, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/api/v1/templates", bytes.NewBuffer(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		}
	})

	// ==========================================
	// STEP B: FETCH ALL TEMPLATES
	// ==========================================
	t.Run("Fetch Templates", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/templates", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var templates map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &templates)

		if body, found := templates["MFA"]; !found || body != "Your code is {{code}}" {
			t.Errorf("expected custom template 'MFA' to be fetched, got templates: %v", templates)
		}
		if body, found := templates["WELCOME"]; !found || body != "Welcome {{name}}!" {
			t.Errorf("expected global default template 'WELCOME' to be present, got templates: %v", templates)
		}
	})

	// ==========================================
	// STEP C: DISPATCH NOTIFICATION
	// ==========================================
	var notificationID string

	t.Run("Dispatch Notification", func(t *testing.T) {
		reqBody := map[string]any{
			"recipient": "user@example.com",
			"type":      "EMAIL",
			"template":  "MFA",
			"variable": map[string]string{
				"code": "12345",
			},
		}
		jsonBytes, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusAccepted {
			t.Errorf("expected status 202, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		id, ok := resp["id"].(string)
		if !ok || id == "" {
			t.Errorf("expected a valid notification ID returned, got: %v", resp)
		}
		notificationID = id
	})

	// ==========================================
	// STEP D: QUERY DISPATCH STATUS
	// ==========================================
	t.Run("Query Dispatch Status", func(t *testing.T) {
		if notificationID == "" {
			t.Skip("No notification ID available from previous step")
		}

		// Wait slightly to let the asynchronous channel dispatch
		time.Sleep(50 * time.Millisecond)

		req, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/notifications/%s", notificationID), nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		status, _ := resp["status"].(string)
		if status == "" {
			t.Errorf("expected status to be populated, got: %s", status)
		}
	})

	// ==========================================
	// STEP E: SCHEDULE AND CALLBACK WEBHOOK DISPATCH
	// ==========================================
	t.Run("Schedule and Callback Webhook", func(t *testing.T) {
		// Mock Scheduler credentials
		system.SchedulerAPIKey = "test_api_key_12345"

		// 1. Create a Scheduled notification (1 hour in the future)
		executeTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
		reqBody := map[string]any{
			"recipient": "user@example.com",
			"type":      "EMAIL",
			"template":  "WELCOME",
			"variable": map[string]string{
				"name": "Jane",
			},
			"execute_at": executeTime,
		}
		jsonBytes, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/api/v1/notifications", bytes.NewBuffer(jsonBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		if w.Code != http.StatusAccepted {
			t.Fatalf("expected status 202, got %d. Body: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		id := resp["id"].(string)
		status := resp["status"].(string)
		if status != "SCHEDULED" {
			t.Errorf("expected status SCHEDULED, got %s", status)
		}

		// 2. Perform Webhook Callback (trigger immediate dispatch)
		callbackReq, _ := http.NewRequest("POST", fmt.Sprintf("/api/v1/internal/notifications/%s/dispatch", id), nil)
		callbackReq.Header.Set("Authorization", "Bearer "+system.SchedulerAPIKey)
		wCallback := httptest.NewRecorder()

		r.ServeHTTP(wCallback, callbackReq)

		if wCallback.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d. Body: %s", wCallback.Code, wCallback.Body.String())
		}

		// 3. Query state to check if it transitioned to PENDING
		queryReq, _ := http.NewRequest("GET", fmt.Sprintf("/api/v1/notifications/%s", id), nil)
		wQuery := httptest.NewRecorder()

		r.ServeHTTP(wQuery, queryReq)

		var queryResp map[string]any
		_ = json.Unmarshal(wQuery.Body.Bytes(), &queryResp)

		finalStatus := queryResp["status"].(string)
		// It might be PENDING, PROCESSING, or SENT depending on how quickly the test worker picks it up
		if finalStatus == "SCHEDULED" {
			t.Errorf("expected status to transition away from SCHEDULED, got %s", finalStatus)
		}
	})
}
