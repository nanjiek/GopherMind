package service

import "time"

func modelRequestTimeout(modelType string) time.Duration {
	if modelType == "kimi" {
		return 600 * time.Second
	}
	return 45 * time.Second
}
