package models

import "time"

type PropertyType string

const (
	PropertyTypeHouse      PropertyType = "house"
	PropertyTypeApartment  PropertyType = "apartment"
	PropertyTypeCondo      PropertyType = "condo"
	PropertyTypeLand       PropertyType = "land"
	PropertyTypeCommercial PropertyType = "commercial"
)

type PropertyStatus string

const (
	PropertyStatusActive  PropertyStatus = "active"
	PropertyStatusPending PropertyStatus = "pending"
	PropertyStatusSold    PropertyStatus = "sold"
	PropertyStatusDeleted PropertyStatus = "deleted"
)

type ListingSource string

const (
	ListingSourceUserGenerated ListingSource = "user_generated"
	ListingSourceMLS           ListingSource = "mls"
	ListingSourceZillow        ListingSource = "zillow"
	ListingSourceRealtor       ListingSource = "realtor"
	ListingSourceOther         ListingSource = "other"
)

type Property struct {
	ID          string         `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Street      string         `json:"street" gorm:"not null"`
	City        string         `json:"city" gorm:"not null"`
	State       string         `json:"state" gorm:"not null"`
	ZipCode     string         `json:"zip_code"`
	Country     string         `json:"country" gorm:"not null"`
	Price       float64        `json:"price" gorm:"not null"`
	Type        PropertyType   `json:"property_type" gorm:"not null"`
	Description string         `json:"description"`
	Bedrooms    *int           `json:"bedrooms"`
	Bathrooms   *float64       `json:"bathrooms"`
	SquareFeet  *int           `json:"square_feet"`
	LotSize     *float64       `json:"lot_size"`
	YearBuilt   *int           `json:"year_built"`
	Latitude    float64        `json:"latitude"`
	Longitude   float64        `json:"longitude"`
	Source      ListingSource  `json:"source" gorm:"not null;default:'user_generated'"`
	SellerID    *string        `json:"seller_id" gorm:"type:uuid"`
	Status      PropertyStatus `json:"status" gorm:"not null;default:'active'"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	Images      []PropertyImage `json:"images" gorm:"foreignKey:PropertyID;constraint:OnDelete:CASCADE"`
	Seller      *User           `json:"seller,omitempty" gorm:"foreignKey:SellerID"`
}

type PropertyImage struct {
	ID         string    `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	PropertyID string    `json:"property_id" gorm:"type:uuid;not null"`
	URL        string    `json:"url" gorm:"not null"`
	Order      int       `json:"order" gorm:"not null;default:0"`
	CreatedAt  time.Time `json:"created_at"`
}
