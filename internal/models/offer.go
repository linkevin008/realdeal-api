package models

import "time"

type OfferStatus string

const (
	OfferStatusPending   OfferStatus = "pending"
	OfferStatusAccepted  OfferStatus = "accepted"
	OfferStatusRejected  OfferStatus = "rejected"
	OfferStatusWithdrawn OfferStatus = "withdrawn"
	// OfferStatusDefaulted: the accepted offer went unpaid past its payment
	// deadline and was reported by the seller (see TrustEventOfferDefault).
	OfferStatusDefaulted OfferStatus = "defaulted"
)

type Offer struct {
	ID         string      `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	PropertyID string      `json:"property_id" gorm:"type:uuid;not null;index"`
	BuyerID    string      `json:"buyer_id" gorm:"type:uuid;not null"`
	Amount     float64     `json:"amount" gorm:"not null"`
	Message    string      `json:"message"`
	Status     OfferStatus `json:"status" gorm:"not null;default:'pending'"`
	// PaymentDeadline is stamped when the offer is accepted; the seller may
	// report non-payment only once this has passed.
	PaymentDeadline *time.Time `json:"payment_deadline,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	Property        *Property  `json:"property,omitempty" gorm:"foreignKey:PropertyID"`
	Buyer           *User      `json:"buyer,omitempty" gorm:"foreignKey:BuyerID"`
}
