package api

import "strconv"

const (
	regionKey     = "region"
	domainKey     = "domain"
	visibilityKey = "visibility"
)

func getVisibility(meta map[string]string) Visibility {
	value, _ := strconv.Atoi(meta[visibilityKey])
	return Visibility(value)
}
