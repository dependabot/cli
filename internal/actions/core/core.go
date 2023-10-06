package core

import "fmt"

// SetSecret hides a string in the Actions logs by outputting a special escape sequence.
func SetSecret(value string) {
	fmt.Println("::add-mask::" + value)
}

// Info logs a message to the Actions log.
func Info(message string) {
	fmt.Println(message)
}

// Error logs an error to the Actions log.
func Error(message string) {
	fmt.Println("::error::" + message)
}

// Warning logs a warning to the Actions log.
func Warning(message string) {
	fmt.Println("::warning::" + message)
}

// StartGroup starts a new collapsible group on the Actions log.
func StartGroup(name string) {
	fmt.Println("::group::" + name)
}

// EndGroup ends the current collapsible group on the Actions log.
func EndGroup() {
	fmt.Println("::endgroup::")
}
