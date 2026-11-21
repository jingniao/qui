package release

import (
	"strconv"
	"strings"
)

// SupportedAgentVersion requires the formal Agent contract shipped in 1.7.5.
func SupportedAgentVersion(value string) bool {
	if !systemUpdateVersion.MatchString(value) {
		return false
	}
	parts := strings.Split(value, ".")
	minimum := [3]uint64{1, 7, 5}
	for index, part := range parts {
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return false
		}
		if number != minimum[index] {
			return number > minimum[index]
		}
	}
	return true
}
