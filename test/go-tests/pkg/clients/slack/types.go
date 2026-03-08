package slack

type ErrorSeverityLevel string

const (
	ErrorSeverityLevelInfo    = "Info"
	ErrorSeverityLevelWarning = "Warning"
	ErrorSeverityLevelError   = "Error"
	ErrorSeverityLevelFatal   = "Fatal"
)

var alertEmojiType = map[ErrorSeverityLevel]string{
	ErrorSeverityLevelInfo:    ":information_source:",
	ErrorSeverityLevelWarning: ":warning:",
	ErrorSeverityLevelError:   ":alert-siren:",
	ErrorSeverityLevelFatal:   ":panic:",
}
