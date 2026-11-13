package convert

import "encoding/json"

func ErrorBody(kind, message string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": map[string]string{"type": kind, "message": message},
	})
	return b
}

func StatusToKind(status int) string {
	switch status {
	case 400:
		return "invalid_request_error"
	case 401:
		return "authentication_error"
	case 403:
		return "permission_error"
	case 404:
		return "not_found_error"
	case 429:
		return "rate_limit_error"
	case 529:
		return "overloaded_error"
	}
	return "api_error"
}
