package models

import (
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type BundleMemeInfo struct {
	TokenAddress string `json:"tokenAddress" bson:"tokenAddress" validate:"required"`
	TokenTicker  string `json:"tokenTicker" bson:"tokenTicker" validate:"required"`
}

type BundleMeta struct {
	BundleName        string `json:"bundleName" bson:"bundleName" validate:"required"`
	BundleBio         string `json:"bundleBio" bson:"bundleBio" validate:"required"`
	BundleDescription string `json:"bundleDescription" bson:"bundleDescription" validate:"required"`
	BundleLogoUrl     string `json:"bundleLogoUrl" bson:"bundleLogoUrl"`
	BundleLevel       int    `json:"bundleLevel" bson:"bundleLevel" `
}

type Bundle struct {
	ID              primitive.ObjectID `json:"_id,omitempty"`
	Bid             int                `json:"_bid" bson:"_bid" validate:"required"`
	BundleMeta      BundleMeta         `json:"bundleMeta" bson:"bundleMeta" validate:"required"`
	BundleAddress   string             `json:"bundleAddress" bson:"bundleAddress"`
	BundleMemesInfo []BundleMemeInfo   `json:"bundleMemesInfo" bson:"bundleMemesInfo"`
	Timestamp       string             `json:"timestamp" bson:"timestamp" validate:"required,numeric"`
}
