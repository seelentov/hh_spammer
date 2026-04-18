package db

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Repository struct {
	db *gorm.DB
}

func New(path string) (*Repository, error) {
	gormDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	if err := gormDB.AutoMigrate(&Filter{}, &Applied{}); err != nil {
		return nil, err
	}
	return &Repository{db: gormDB}, nil
}

func (r *Repository) AddFilter(rawURL, name string) error {
	return r.db.Create(&Filter{URL: rawURL, Name: name, Active: true}).Error
}

func (r *Repository) GetActiveFilters() ([]Filter, error) {
	var list []Filter
	return list, r.db.Where("active = ?", true).Find(&list).Error
}

func (r *Repository) GetAllFilters() ([]Filter, error) {
	var list []Filter
	return list, r.db.Find(&list).Error
}

func (r *Repository) DeleteFilter(id uint) error {
	return r.db.Delete(&Filter{}, id).Error
}

func (r *Repository) ToggleFilter(id uint, active bool) error {
	return r.db.Model(&Filter{}).Where("id = ?", id).Update("active", active).Error
}

func (r *Repository) IsApplied(vacancyID string) bool {
	var count int64
	r.db.Model(&Applied{}).Where("vacancy_id = ?", vacancyID).Count(&count)
	return count > 0
}

func (r *Repository) SaveApplied(a *Applied) error {
	return r.db.Create(a).Error
}

func (r *Repository) GetApplied(limit int) ([]Applied, error) {
	var list []Applied
	q := r.db.Order("applied_at desc")
	if limit > 0 {
		q = q.Limit(limit)
	}
	return list, q.Find(&list).Error
}

func (r *Repository) CountApplied() (int64, error) {
	var count int64
	return count, r.db.Model(&Applied{}).Where("success = ?", true).Count(&count).Error
}
