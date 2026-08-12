package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

type upstreamSemanticFailure struct {
	Code    string
	Message string
	Body    []byte
}

func (e *upstreamSemanticFailure) Error() string {
	if e == nil {
		return "upstream response declared a failure"
	}
	detail := nonEmptyString(e.Message, e.Code, "unknown error")
	if e.Code != "" && e.Message != "" {
		detail = fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("upstream response failed: %s", detail)
}

func semanticFailureFromJSON(body []byte) (*upstreamSemanticFailure, bool) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, false
	}
	return semanticFailureFromObject(root, body)
}

func semanticFailureFromObject(root map[string]any, body []byte) (*upstreamSemanticFailure, bool) {
	if root == nil {
		return nil, false
	}

	eventType := strings.ToLower(strings.TrimSpace(anyString(root["type"])))
	status := strings.ToLower(strings.TrimSpace(anyString(root["status"])))
	response, _ := root["response"].(map[string]any)
	responseStatus := strings.ToLower(strings.TrimSpace(anyString(response["status"])))
	topLevelError, hasTopLevelError := root["error"]
	failed := (hasTopLevelError && topLevelError != nil) || eventType == "response.failed" || eventType == "response.error" || eventType == "error" ||
		status == "failed" || status == "error" || responseStatus == "failed" || responseStatus == "error"
	if !failed {
		return nil, false
	}

	errorValue := root["error"]
	if errorValue == nil && response != nil {
		errorValue = response["error"]
	}
	code, message := semanticFailureDetails(errorValue)
	code = nonEmptyString(code, eventType, status, responseStatus, "upstream_response_failed")
	message = nonEmptyString(message, "upstream response declared a failure")
	return &upstreamSemanticFailure{Code: code, Message: message, Body: append([]byte(nil), body...)}, true
}

func semanticFailureDetails(value any) (string, string) {
	switch typed := value.(type) {
	case map[string]any:
		return strings.TrimSpace(anyString(typed["code"])), strings.TrimSpace(anyString(typed["message"]))
	case string:
		var nested map[string]any
		if json.Unmarshal([]byte(typed), &nested) == nil {
			return semanticFailureDetails(nested)
		}
		return "", strings.TrimSpace(typed)
	default:
		return "", ""
	}
}

func semanticFailureClassificationBody(failure *upstreamSemanticFailure) []byte {
	if failure == nil {
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    failure.Code,
			"message": failure.Message,
		},
	})
	return body
}
