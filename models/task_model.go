// models/task_model.go
package models

import (
	"time"
)

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusProcessing TaskStatus = "processing"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
)

type Task struct {
	ID          string      `json:"id" bson:"_id"`
	Type        string      `json:"type" bson:"type"`
	Status      TaskStatus  `json:"status" bson:"status"`
	Payload     interface{} `json:"payload" bson:"payload"`
	Result      interface{} `json:"result" bson:"result"`
	Error       string      `json:"error" bson:"error"`
	CreatedAt   time.Time   `json:"createdAt" bson:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt" bson:"updatedAt"`
	CompletedAt *time.Time  `json:"completedAt" bson:"completedAt"`
}

type BuyTokensTaskPayload struct {
	BundleID       int      `json:"bundleId"`
	UserPublicKey  string   `json:"userPublicKey"`
	SolAmount      float64  `json:"solAmount"`
	TransactionIDs []string `json:"transactionIds"`
	TokenAddresses []string `json:"tokenAddresses"`
}
