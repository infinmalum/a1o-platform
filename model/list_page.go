package model

import "gorm.io/gorm"

// ListPage limits Studio list queries. A zero limit preserves existing callers.
type ListPage struct {
	Offset int
	Limit  int
}

func (page ListPage) Apply(query *gorm.DB) *gorm.DB {
	if page.Limit == 0 {
		return query
	}
	return query.Offset(page.Offset).Limit(page.Limit)
}
