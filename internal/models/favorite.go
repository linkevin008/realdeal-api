package models

import "time"

type Favorite struct {
	ID         string    `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID     string    `json:"user_id" gorm:"type:uuid;not null;uniqueIndex:idx_user_property"`
	PropertyID string    `json:"property_id" gorm:"type:uuid;not null;uniqueIndex:idx_user_property"`
	SavedAt    time.Time `json:"saved_at"`
	Property   *Property `json:"property,omitempty" gorm:"foreignKey:PropertyID"`
}
