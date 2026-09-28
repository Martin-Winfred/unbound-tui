package domain

// Controller is the Unbound runtime control abstraction. The tool only
// reloads the daemon and reads its state; it never injects records at
// runtime, so the interface stays deliberately small.
type Controller interface {
	Reload() error
	Status() (StatusInfo, error)
	ListLocalZones() ([]LocalZone, error)
	ListLocalData() ([]string, error)
}
