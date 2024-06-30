package ghappregstate

//go:generate stringer -type Enum
type Enum int

const (
	Unconfigured Enum = iota
	FetchingAppData
	Configured
	Unknown
)

// Convenience method for use in templates
func (e Enum) Is(value string) bool {
	return e.String() == value
}
