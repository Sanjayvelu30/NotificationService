package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/sanjay/NotificationService/internal/scheduler/config"
	"github.com/sanjay/NotificationService/internal/scheduler/domain"
	"github.com/sanjay/NotificationService/internal/scheduler/handler"
	"github.com/sanjay/NotificationService/internal/scheduler/repository"
	"github.com/sanjay/NotificationService/internal/scheduler/service"
)

var (
	testDB     *sql.DB
	testRdb    *redis.Client
	testRepo   repository.TaskRepo
	testDLQ    repository.DLQRepo
	testAPIKey = "test_api_key_12345"
)

func TestMain(m *testing.M) {
	// Load configuration properties
	cfg, err := config.Load()
	if err != nil {
		logPrintf("Skipping E2E tests: failed to load config: %v", err)
		os.Exit(0)
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		logPrintf("Skipping E2E tests: failed to init DB connection: %v", err)
		os.Exit(0)
	}
	testDB = db

	// Verify DB is reachable
	if err := db.Ping(); err != nil {
		logPrintf("Skipping E2E tests: Database is unreachable (run docker compose up db): %v", err)
		os.Exit(0)
	}

	// Setup Redis
	redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: cfg.RedisPassword,
		DB:       0,
	})
	testRdb = rdb

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		logPrintf("Skipping E2E tests: Redis is unreachable (run docker compose up redis): %v", err)
		os.Exit(0)
	}

	testRepo = repository.NewPostgresRepo(db, rdb)
	testDLQ = repository.NewPostgresDLQRepository(db)

	// Run tests
	code := m.Run()

	// Clean up
	db.Close()
	rdb.Close()
	os.Exit(code)
}

func logPrintf(format string, v ...interface{}) {
	fmt.Printf(format+"\n", v...)
}

func setupTestTables(t *testing.T) {
	// Recreate schema to isolate tests
	_, err := testDB.Exec(`
		DROP TABLE IF EXISTS tasks;
		DROP TABLE IF EXISTS dlq_tasks;

		CREATE TABLE tasks (
			id VARCHAR(255) PRIMARY KEY,
			title VARCHAR(255) NOT NULL,
			description TEXT,
			callback_url TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL DEFAULT '',
			status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
			retry_count INT NOT NULL DEFAULT 0,
			max_retries INT NOT NULL DEFAULT 3,
			execute_at TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL
		);

		CREATE TABLE dlq_tasks (
			id VARCHAR(255) PRIMARY KEY,
			task_id VARCHAR(255) NOT NULL,
			title VARCHAR(255),
			description TEXT,
			callback_url TEXT,
			payload TEXT,
			failed_at TIMESTAMP WITH TIME ZONE NOT NULL,
			retry_count INT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("Failed to setup test tables: %v", err)
	}

	// Flush Redis cache
	err = testRdb.FlushDB(context.Background()).Err()
	if err != nil {
		t.Fatalf("Failed to flush redis: %v", err)
	}
}

func TestE2E_Health(t *testing.T) {
	setupTestTables(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	taskHandler := handler.NewHandler(testRepo)
	taskHandler.RegisterRoutes(router, testAPIKey)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestE2E_Authentication(t *testing.T) {
	setupTestTables(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	taskHandler := handler.NewHandler(testRepo)
	taskHandler.RegisterRoutes(router, testAPIKey)

	// 1. Without Token
	reqBody := `{"title": "Test Auth"}`
	req := httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", w.Code)
	}

	// 2. With Invalid Token
	req = httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid_token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for invalid token, got %d", w.Code)
	}

	// 3. With Valid Token
	req = httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201 Created for valid token, got %d", w.Code)
	}
}

func TestE2E_CreateAndGetTask(t *testing.T) {
	setupTestTables(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	taskHandler := handler.NewHandler(testRepo)
	taskHandler.RegisterRoutes(router, testAPIKey)

	taskReq := `{"title": "Get Task Test", "description": "Ensure get works"}`
	req := httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(taskReq))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create task: %d", w.Code)
	}

	var createdTask domain.Task
	_ = json.Unmarshal(w.Body.Bytes(), &createdTask)

	// Fetch task
	reqGet := httptest.NewRequest("GET", "/tasks/"+createdTask.ID, nil)
	reqGet.Header.Set("Authorization", "Bearer "+testAPIKey)
	wGet := httptest.NewRecorder()
	router.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Errorf("Expected GET status 200, got %d", wGet.Code)
	}

	var fetchedTask domain.Task
	_ = json.Unmarshal(wGet.Body.Bytes(), &fetchedTask)

	if fetchedTask.Title != "Get Task Test" {
		t.Errorf("Expected Title 'Get Task Test', got %s", fetchedTask.Title)
	}
}

func TestE2E_CreateTask_Idempotency(t *testing.T) {
	setupTestTables(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	taskHandler := handler.NewHandler(testRepo)
	taskHandler.RegisterRoutes(router, testAPIKey)

	taskID := "idempotent-task-uuid"
	taskReq := fmt.Sprintf(`{"id": "%s", "title": "Idempotent Task"}`, taskID)

	// First request
	req1 := httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(taskReq))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+testAPIKey)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("First create failed: status %d", w1.Code)
	}

	// Second request
	req2 := httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(taskReq))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+testAPIKey)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for idempotent duplicate request, got %d", w2.Code)
	}

	var fetchedTask domain.Task
	_ = json.Unmarshal(w2.Body.Bytes(), &fetchedTask)
	if fetchedTask.ID != taskID {
		t.Errorf("Expected response task ID %s, got %s", taskID, fetchedTask.ID)
	}
}

func TestE2E_TaskExecutionFlow_Success(t *testing.T) {
	setupTestTables(t)
	gin.SetMode(gin.TestMode)

	// 1. Setup mock server to receive callback
	callbackReceived := make(chan string, 1)
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		callbackReceived <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	// 2. Create task pointing to mock server
	taskID := "task-callback-success"
	taskBody := fmt.Sprintf(`{
		"id": "%s",
		"title": "Success Callback Test",
		"callback_url": "%s",
		"payload": "{\"status\":\"ok\"}"
	}`, taskID, mockServer.URL)

	router := gin.New()
	taskHandler := handler.NewHandler(testRepo)
	taskHandler.RegisterRoutes(router, testAPIKey)

	req := httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(taskBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create task: %d", w.Code)
	}



	// 3. Start scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := service.NewTaskScheduler(testRepo, testDLQ, 1, 5)
	scheduler.Start(ctx)
	defer scheduler.Stop()

	// 4. Verify callback is received
	select {
	case payload := <-callbackReceived:
		if !strings.Contains(payload, `"status":"ok"`) {
			t.Errorf("Expected payload to contain status:ok, got: %s", payload)
		}
	case <-time.After(3 * time.Second):
		// Dump database tasks table for diagnosis
		rows, err := testDB.Query("SELECT id, status, execute_at, created_at FROM tasks")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, status string
				var execAt, creatAt time.Time
				_ = rows.Scan(&id, &status, &execAt, &creatAt)
				t.Errorf("TIMEOUT DB STATE: id=%s, status=%s, execute_at=%v, created_at=%v", id, status, execAt, creatAt)
			}
		}
		t.Fatal("Timeout waiting for callback request")
	}

	// 5. Verify status in database is COMPLETED
	time.Sleep(200 * time.Millisecond) // wait for database update
	dbTask, err := testRepo.Get(taskID)
	if err != nil {
		t.Fatalf("Failed to query task from DB: %v", err)
	}
	if dbTask.Status != domain.StatusCompleted {
		t.Errorf("Expected status COMPLETED, got %s", dbTask.Status)
	}
}

func TestE2E_TaskExecutionFlow_RetryAndDLQ(t *testing.T) {
	setupTestTables(t)
	gin.SetMode(gin.TestMode)

	// 1. Setup mock server returning 500 error to force failures
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	// 2. Create task with max_retries = 1 (1 original attempt + 1 retry)
	taskID := "task-callback-fail"
	taskBody := fmt.Sprintf(`{
		"id": "%s",
		"title": "Failed Retry Callback Test",
		"callback_url": "%s",
		"max_retries": 1
	}`, taskID, mockServer.URL)

	router := gin.New()
	taskHandler := handler.NewHandler(testRepo)
	taskHandler.RegisterRoutes(router, testAPIKey)

	req := httptest.NewRequest("POST", "/tasks", bytes.NewBufferString(taskBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create task: %d", w.Code)
	}

	// 3. Start scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := service.NewTaskScheduler(testRepo, testDLQ, 1, 5)
	scheduler.Start(ctx)
	defer scheduler.Stop()

	// Wait for processing: execution 1 (fails, backoff schedules in 4s), then execution 2 (fails, goes to DLQ)
	// Let's wait 6.5 seconds to cover both attempts and scheduler ticks.
	time.Sleep(6500 * time.Millisecond)

	// 4. Verify status in database is FAILED
	dbTask, err := testRepo.Get(taskID)
	if err != nil {
		t.Fatalf("Failed to query task from DB: %v", err)
	}
	if dbTask.Status != domain.StatusFailed {
		t.Errorf("Expected status FAILED, got %s", dbTask.Status)
	}

	// 5. Verify task is recorded in DLQ database table
	var count int
	err = testDB.QueryRow("SELECT COUNT(*) FROM dlq_tasks WHERE task_id = $1", taskID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check DLQ db table: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 task in DLQ table, got %d", count)
	}
}
