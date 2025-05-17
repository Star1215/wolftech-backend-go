package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type TokenAllocation struct {
	Mint          string  `json:"mint" bson:"mint"`
	Amount        string  `json:"amount" bson:"amount"`
	PendingAmount string  `json:"pendingAmount" bson:"pendingAmount"`
	PurchasedSOL  float64 `json:"purchasedSol" bson:"purchasedSol"`
	Ticker        string  `json:"ticker" bson:"ticker,omitempty"`
	Pending       bool    `json:"pending" bson:"pending"`
}

type UserHolding struct {
	BundleID    int               `json:"bundleId" bson:"bundleId"`
	BundleName  string            `json:"bundleName" bson:"bundleName,omitempty"`
	InvestedSOL float64           `json:"investedSol" bson:"investedSol"`
	Tokens      []TokenAllocation `json:"tokens" bson:"tokens"`
	Pending     bool              `json:"pending" bson:"pending"`
	Active      bool              `json:"active" bson:"active"`
	Timestamp   time.Time         `json:"timestamp" bson:"timestamp"`
}

type UserDeposit struct {
	ID               primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	Wallet           string             `json:"wallet" bson:"wallet"`
	TotalInvestedSOL float64            `json:"totalInvestedSol" bson:"totalInvestedSol"`
	Deposits         []UserHolding      `json:"deposits" bson:"deposits"`
	Pending          bool               `json:"pending" bson:"pending"`
	LastUpdated      time.Time          `json:"lastUpdated" bson:"lastUpdated"`
}
