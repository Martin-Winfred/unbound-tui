package domain

// Store 是持久层抽象（依赖倒置：编排层与 TUI 层只依赖此接口）
type Store interface {
	ListZones() ([]Zone, error)
	ListAllRecords() ([]Record, error)
	ListRecordsPaged(zone string, page, pageSize int) ([]Record, error)
	AddRecord(r Record) error
	BulkAddRecords(records []Record) error
	UpdateRecord(id int64, ttl int) error
	DeleteRecord(id int64) error
	DeleteZoneAndRecords(zone string) error
	GetStats() (StoreStats, error)
	Close() error
}

// Controller 是 Unbound 运行时控制抽象
type Controller interface {
	AddRecord(rr string) error            // local_data：单条 RR
	RemoveRecord(rr string) error         // local_data_remove：单条 RR
	BulkAddRRs(rrs []string) error        // local_datas：stdin 每行一条 RR
	BulkRemoveNames(names []string) error // local_datas_remove：stdin 每行一个名称
	AddZone(name, typ string) error       // local_zone
	RemoveZone(name string) error         // local_zone_remove（连同 zone 内全部记录）
	Reload() error
	Status() (StatusInfo, error)
}
