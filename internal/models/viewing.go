package models

import "time"

type ViewingRequestStatus string

const (
	ViewingRequestStatusPending   ViewingRequestStatus = "pending"
	ViewingRequestStatusAccepted  ViewingRequestStatus = "accepted"
	ViewingRequestStatusDeclined  ViewingRequestStatus = "declined"
	ViewingRequestStatusCancelled ViewingRequestStatus = "cancelled"
)

// ViewingSlot is a one-off dated time window a seller posts on their listing
// for buyers to request a viewing against. No recurrence — each slot is a
// single dated occurrence.
type ViewingSlot struct {
	ID         string    `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	PropertyID string    `json:"property_id" gorm:"type:uuid;not null;index"`
	StartTime  time.Time `json:"start_time" gorm:"not null"`
	EndTime    time.Time `json:"end_time" gorm:"not null"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Property   *Property `json:"property,omitempty" gorm:"foreignKey:PropertyID"`
}

// ViewingRequest is a buyer's request to view a property during a specific
// slot. The seller approves (pending -> accepted/declined); only one buyer
// per slot may hold an accepted request at a time.
type ViewingRequest struct {
	ID         string               `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SlotID     string               `json:"slot_id" gorm:"type:uuid;not null;index"`
	PropertyID string               `json:"property_id" gorm:"type:uuid;not null;index"`
	BuyerID    string               `json:"buyer_id" gorm:"type:uuid;not null"`
	Message    string               `json:"message"`
	Status     ViewingRequestStatus `json:"status" gorm:"not null;default:'pending'"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
	Slot       *ViewingSlot         `json:"slot,omitempty" gorm:"foreignKey:SlotID"`
	Buyer      *User                `json:"buyer,omitempty" gorm:"foreignKey:BuyerID"`
	Property   *Property            `json:"property,omitempty" gorm:"foreignKey:PropertyID"`
}
