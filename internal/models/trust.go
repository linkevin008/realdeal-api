package models

import "time"

// TrustEventType identifies the kind of violation a trust event records.
type TrustEventType string

const (
	// TrustEventOfferDefault: an accepted offer went unpaid past the payment
	// deadline. Seller-reported, objectively verified by the server (deadline
	// elapsed), auto-confirmed.
	TrustEventOfferDefault TrustEventType = "offer_default"
	// TrustEventDeedDefault: the seller failed to deliver the deed after
	// acceptance. Buyer-reported, not machine-verifiable — lands pending_review.
	TrustEventDeedDefault TrustEventType = "deed_default"
	// TrustEventDocumentFraud: the seller provided inauthentic documents.
	// Buyer-reported, not machine-verifiable — lands pending_review.
	TrustEventDocumentFraud TrustEventType = "document_fraud"
)

// TrustEventStatus tracks a report through manual adjudication (or
// auto-confirmation for objectively verifiable events).
type TrustEventStatus string

const (
	TrustEventStatusPendingReview TrustEventStatus = "pending_review"
	TrustEventStatusConfirmed     TrustEventStatus = "confirmed"
	TrustEventStatusDismissed     TrustEventStatus = "dismissed"
)

// TrustEvent is a hidden, per-account trustworthiness record. It is never
// serialized or exposed through any endpoint — every field is json:"-".
// A single confirmed event is sufficient to enforce a block; there is no
// strike count.
type TrustEvent struct {
	ID         string           `json:"-" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID     string           `json:"-" gorm:"type:uuid;not null;index"`
	EventType  TrustEventType   `json:"-" gorm:"not null;uniqueIndex:idx_offer_event_type"`
	Status     TrustEventStatus `json:"-" gorm:"not null;default:'pending_review'"`
	Severity   int              `json:"-" gorm:"not null;default:100"`
	OfferID    *string          `json:"-" gorm:"type:uuid;uniqueIndex:idx_offer_event_type"`
	PropertyID *string          `json:"-" gorm:"type:uuid"`
	ReportedBy *string          `json:"-" gorm:"type:uuid"`
	Notes      string           `json:"-"`
	CreatedAt  time.Time        `json:"-"`
}

// TrustAppealStatus tracks an appeal through manual adjudication. There is
// no code path that transitions an appeal — ops does this directly against
// the database (see the SQL on TrustAppeal below).
type TrustAppealStatus string

const (
	TrustAppealStatusPending    TrustAppealStatus = "pending"
	TrustAppealStatusUpheld     TrustAppealStatus = "upheld"
	TrustAppealStatusOverturned TrustAppealStatus = "overturned"
)

// TrustAppeal is a hidden, per-event appeal of a confirmed trust event. Like
// TrustEvent, it is trust data: never serialized, never exposed through any
// GET endpoint — every field is json:"-". There are no adjudication
// endpoints; resolution is a manual ops action performed directly against
// the database.
//
// Manual ops SQL:
//
//	-- Uphold an appeal (block stays; appeal is closed):
//	UPDATE trust_appeals SET status = 'upheld', resolved_at = now()
//	WHERE id = '<appeal_id>' AND status = 'pending';
//
//	-- Overturn an appeal (block lifts automatically — enforcement is
//	-- derived from confirmed trust events, so dismissing the linked event
//	-- is the only step needed; no other code path re-checks this):
//	BEGIN;
//	UPDATE trust_appeals SET status = 'overturned', resolved_at = now()
//	WHERE id = '<appeal_id>' AND status = 'pending';
//	UPDATE trust_events SET status = 'dismissed'
//	WHERE id = (SELECT trust_event_id FROM trust_appeals WHERE id = '<appeal_id>');
//	COMMIT;
type TrustAppeal struct {
	ID           string            `json:"-" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	TrustEventID string            `json:"-" gorm:"column:trust_event_id;type:uuid;not null;uniqueIndex"`
	UserID       string            `json:"-" gorm:"type:uuid;not null;index"`
	Statement    string            `json:"-" gorm:"not null"`
	Status       TrustAppealStatus `json:"-" gorm:"not null;default:'pending'"`
	ResolvedAt   *time.Time        `json:"-"`
	CreatedAt    time.Time         `json:"-"`
}
