package models

import (
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type MemeSocials struct {
	Website  string `json:"website" bson:"website"`
	Twitter  string `json:"twitter" bson:"twitter"`
	Telegram string `json:"telegram" bson:"telegram"`
}

type Meme struct {
	ID          primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	Bid         string             `json:"_bid" bson:"_bid" validate:"required"`
	MemeName    string             `json:"memeName" bson:"memeName" validate:"required"`
	MemeTicker  string             `json:"memeTicker" bson:"memeTicker" validate:"required"`
	MemeAddress string             `json:"memeAddress" bson:"memeAddress" validate:"required"`
	MemeLogo    string             `json:"memeLogo" bson:"memeLogo"`
	MemeSocials MemeSocials        `json:"memeSocials" bson:"memeSocials"`
	CreatedTs   string             `json:"createdTs" bson:"createdTs" validate:"required"`
	UpdatedTs   string             `json:"updatedTs" bson:"updatedTs"`
}
