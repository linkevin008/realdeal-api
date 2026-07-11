package models

import "time"

type ContractStatus string

const (
	ContractStatusDraft        ContractStatus = "draft"
	ContractStatusTermsAgreed  ContractStatus = "terms_agreed"
	ContractStatusBuyerSigned  ContractStatus = "buyer_signed"
	ContractStatusSellerSigned ContractStatus = "seller_signed"
	ContractStatusExecuted     ContractStatus = "executed"
	ContractStatusCancelled    ContractStatus = "cancelled"
	ContractStatusExpired      ContractStatus = "expired"
)

// Contract is the post-acceptance signing flow for an accepted offer (MVP
// step 5 of the flow). It is created automatically inside AcceptOffer's
// transaction — there is no create endpoint. The documents themselves are
// stubbed for the MVP; only the state machine (terms -> agreement -> both
// signatures -> executed) is real.
//
// Terms flow: either party proposes terms (move-in/transfer dates and
// conditions); the other party agrees. Signing only unlocks once both
// parties have agreed to the CURRENT terms — any terms change voids the
// other party's agreement and both signatures, forcing re-agreement and
// re-signing.
//
// Expiry is evaluated lazily (no background jobs): every contract endpoint
// checks ExecutionDeadline against now before acting on a non-terminal
// contract. If the deadline has passed, the contract flips to expired and
// the property reverts pending -> active in the same transaction.
//
// NOTE (future hook): expiry currently carries no trust consequence by
// product decision — penalties for a contract that expired without
// execution will be layered on later via the trust_events core (see
// internal/models/trust.go), the same way offer non-payment is handled.
type Contract struct {
	ID         string `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OfferID    string `json:"offer_id" gorm:"type:uuid;not null;uniqueIndex"`
	PropertyID string `json:"property_id" gorm:"type:uuid;not null;index"`
	// SellerID/BuyerID are denormalized from the offer/property at creation
	// time and are immutable afterward — they identify the two contract
	// parties even if, e.g., a property's seller_id were ever reassigned.
	SellerID string         `json:"seller_id" gorm:"type:uuid;not null;index"`
	BuyerID  string         `json:"buyer_id" gorm:"type:uuid;not null;index"`
	Status   ContractStatus `json:"status" gorm:"not null;default:'draft'"`

	// Terms — set together whenever either party proposes/updates them.
	MoveInDate   *time.Time `json:"move_in_date"`
	TransferDate *time.Time `json:"transfer_date"`
	Conditions   string     `json:"conditions"`

	// TermsProposedBy is the user id of whichever party most recently
	// proposed the current terms; nil until terms are proposed at least once.
	TermsProposedBy *string `json:"terms_proposed_by"`

	// Agreement — set when each party agrees to the CURRENT terms. Any terms
	// change resets the non-proposer's AgreedAt to nil (the proposer is
	// implicitly agreeing by proposing) and resets both signatures.
	BuyerAgreedAt  *time.Time `json:"buyer_agreed_at"`
	SellerAgreedAt *time.Time `json:"seller_agreed_at"`

	// Signatures — only settable once both parties have agreed to the
	// current terms (status terms_agreed or beyond).
	BuyerSignedAt  *time.Time `json:"buyer_signed_at"`
	SellerSignedAt *time.Time `json:"seller_signed_at"`

	// ExecutionDeadline is stamped as an absolute timestamp at creation
	// (now + CONTRACT_EXECUTION_DEADLINE_DAYS). Past this, and not yet
	// executed/cancelled, the contract expires on next access.
	ExecutionDeadline time.Time `json:"execution_deadline"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Offer    *Offer    `json:"offer,omitempty" gorm:"foreignKey:OfferID"`
	Property *Property `json:"property,omitempty" gorm:"foreignKey:PropertyID"`
	Seller   *User     `json:"seller,omitempty" gorm:"foreignKey:SellerID"`
	Buyer    *User     `json:"buyer,omitempty" gorm:"foreignKey:BuyerID"`
}
