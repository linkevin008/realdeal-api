package models

import "time"

type UserRole string

const (
	UserRoleBuyer  UserRole = "buyer"
	UserRoleSeller UserRole = "seller"
	UserRoleBoth   UserRole = "both"
)

type User struct {
	ID              string    `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Name            string    `json:"name" gorm:"not null"`
	Email           string    `json:"email" gorm:"uniqueIndex;not null"`
	PasswordHash    string    `json:"-" gorm:"not null"`
	AppleID         *string   `json:"-" gorm:"uniqueIndex"`
	GoogleID        *string   `json:"-" gorm:"uniqueIndex"`
	PhoneNumber     *string   `json:"phone_number"`
	ProfilePhotoURL *string   `json:"profile_photo_url"`
	Role            UserRole  `json:"role" gorm:"not null;default:'buyer'"`
	ShowEmail       bool      `json:"show_email" gorm:"not null;default:true"`
	ShowPhone       bool      `json:"show_phone" gorm:"not null;default:true"`
	ShowListings    bool      `json:"show_listings" gorm:"not null;default:true"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
