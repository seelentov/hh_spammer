package db

import "time"

type Filter struct {
	ID        uint      `gorm:"primaryKey"`
	URL       string    `gorm:"not null"`
	Name      string
	Active    bool      `gorm:"default:true"`
	CreatedAt time.Time
}

type Applied struct {
	ID        uint      `gorm:"primaryKey"`
	VacancyID string    `gorm:"uniqueIndex;not null"`
	URL       string
	Title     string
	Company   string
	AppliedAt time.Time
	Success   bool
	ErrorMsg  string
}
