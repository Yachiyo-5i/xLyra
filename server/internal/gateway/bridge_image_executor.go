package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

const (
	bridgeImageRequestTimeout      = 5 * time.Minute
	bridgeImageMaxAttempts         = 3
	bridgeImageMaxAmbiguousRetries = 1
)

type bridgeImageOutcome struct {
	OK            bool
	B64           string
	RevisedPrompt string
	ErrorMessage  string
}

type noopResponseWriter struct {
	header http.Header
}

func newNoopResponseWriter() *noopResponseWriter {
	return &noopResponseWriter{header: http.Header{}}
}

func (w *noopResponseWriter) Header() http.Header         { return w.header }
func (w *noopResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *noopResponseWriter) WriteHeader(int)             {}

func (h Handler) executeBridgeImageGeneration(
	ctx context.Context,
	requestID string,
	apiKeyID uuid.UUID,
	cfg store.APIKeyImageBridge,
	prompt string,
	spec bridgeImageToolSpec,
) bridgeImageOutcome {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return bridgeImageOutcome{ErrorMessage: "the generate_image call did not include a prompt"}
	}

	ctx, cancel := context.WithTimeout(ctx, bridgeImageRequestTimeout)
	defer cancel()

	query := routeengine.CandidateQuery{
		ModelKey:      cfg.Model,
		EndpointType:  upstreamEndpointTypeOpenAIImage,
		FailoverLimit: 3,
	}
	if cfg.SiteID != nil {
		query.AllowedSiteIDs = []uuid.UUID{*cfg.SiteID}
	}
	plan, err := h.router.Plan(ctx, query)
	if err != nil {
		return bridgeImageOutcome{ErrorMessage: fmt.Sprintf("no route for image model %s: %v", cfg.Model, err)}
	}

	payload := map[string]any{
		"model":  cfg.Model,
		"prompt": prompt,
		"n":      1,
	}
	if spec.Size != "" {
		payload["size"] = spec.Size
	}
	if spec.Quality != "" {
		payload["quality"] = spec.Quality
	}
	if spec.OutputFormat != "" {
		payload["output_format"] = spec.OutputFormat
	}
	request := gatewayRequest{
		DownstreamPath:    gatewayEndpointImagesGenerations,
		DownstreamHeaders: http.Header{},
		RequestedModel:    cfg.Model,
		Stream:            false,
		Payload:           payload,
	}
	resolver := openAIProtocolResolver{db: h.db}

	attempts := append([]routeengine.Candidate{plan.Selected}, plan.Failover...)
	ambiguousRetries := 0
	totalAttempts := 0
	var lastError string
	for index, candidate := range attempts {
		if totalAttempts >= bridgeImageMaxAttempts {
			break
		}
		totalAttempts++

		protocol, resolveErr := resolver.Resolve(ctx, request, candidate)
		if resolveErr != nil {
			lastError = resolveErr.Error()
			continue
		}
		result := h.forwardGatewayRequest(ctx, newNoopResponseWriter(), requestID, index+1, apiKeyID, plan.CanonicalModel.ID, candidate, request, nil, protocol)
		if result.success {
			h.clearCooldownAfterRecovery(ctx, candidate)
			outcome := bridgeImageOutcomeFromBody(result.body)
			if outcome.OK {
				return outcome
			}
			lastError = outcome.ErrorMessage
			continue
		}
		lastError = nonEmptyString(result.errorMessage, result.errorType)
		if ctx.Err() != nil {
			break
		}
		h.cooldownAfterFailure(ctx, candidate, result)
		if bridgeImageFailureIsAmbiguous(result.errorType) {
			if ambiguousRetries >= bridgeImageMaxAmbiguousRetries {
				break
			}
			ambiguousRetries++
		}
	}
	if h.logger != nil {
		h.logger.WarnContext(ctx, "image bridge generation failed",
			"scope", "gateway", "request_id", requestID, "model", cfg.Model,
			"attempts", totalAttempts, "error", lastError)
	}
	return bridgeImageOutcome{ErrorMessage: nonEmptyString(lastError, "image generation failed")}
}

func bridgeImageFailureIsAmbiguous(errorType string) bool {
	switch strings.TrimSpace(errorType) {
	case "upstream_timeout", "upstream_response_read_failed", "upstream_stream_failed":
		return true
	default:
		return false
	}
}

func bridgeImageOutcomeFromBody(body []byte) bridgeImageOutcome {
	var response struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return bridgeImageOutcome{ErrorMessage: "image response could not be parsed"}
	}
	for _, item := range response.Data {
		if strings.TrimSpace(item.B64JSON) != "" {
			return bridgeImageOutcome{OK: true, B64: item.B64JSON, RevisedPrompt: item.RevisedPrompt}
		}
	}
	return bridgeImageOutcome{ErrorMessage: "image response did not include base64 image data"}
}
