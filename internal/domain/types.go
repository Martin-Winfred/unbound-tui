package domain

import "time"

type Zone struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Record struct {
	ID        int64     `json:"id"`
	Zone      string    `json:"zone"`
	Name      string    `json:"name"`
	RType     string    `json:"rtype"`
	Value     string    `json:"value"`
	TTL       int       `json:"ttl"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type StoreStats struct {
	TotalZones   int `json:"total_zones"`
	TotalRecords int `json:"total_records"`
	ActiveZones  int `json:"active_zones"`
}

// StatusInfo 是 unbound-control status 的解析结果
type StatusInfo struct {
	Version     string // "1.26.0"
	ControlType string // "namedpipe" | "ssl" | ""（未启用）
	Raw         string
}
