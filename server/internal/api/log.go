package api

import "log"

func writeLogf(format string, args ...any) {
	log.Printf(format, args...)
}
