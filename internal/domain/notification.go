package domain

import "time"

var Templates = map[string]string{
	"WELCOME":        "Welcome to our service!",
	"MFA":            "Your multi-factor authentication code is {{code}}.",
	"PASSWORD_RESET": "Click here to reset your password: {{link}}.",
}

type User struct {
	ID string
}

type NotificationType string

const (
	Email NotificationType = "EMAIL"
	Sms   NotificationType = "SMS"
	Push  NotificationType = "PUSH"
)

type NotificationStatus string

const (
	Pending    NotificationStatus = "PENDING"
	Processing NotificationStatus = "PROCESSING"
	Sent       NotificationStatus = "SENT"
	DLQ        NotificationStatus = "DLQ"
	Scheduled  NotificationStatus = "SCHEDULED"
)

type Notification struct {
	ID          string            `json:"id"`
	Recipient   string            `json:"recipient"`
	Template    string            `json:"template"`
	Variable    map[string]string `json:"variable"`
	RetryCount  int               `json:"retry_count"`
	CreatedAt   time.Time         `json:"created_at"`
	Type        NotificationType  `json:"type"`
	Status      NotificationStatus `json:"status"`
	NextRetryAt time.Time         `json:"next_retry_at"`
	UserID      string            `json:"user_id"`
	ErrorMessage string           `json:"error_message"`
}
