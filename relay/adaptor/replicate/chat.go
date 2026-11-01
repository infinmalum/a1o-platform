package replicate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/adaptor/openai"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
	"github.com/infinmalum/a1o-platform/relay/sse"
	"github.com/infinmalum/a1o-platform/relay/wire"
	"github.com/pkg/errors"
)

const (
	eventPrefix = "event:"
	dataPrefix  = "data:"
)

// ChatHandler runs the prediction: create, poll until it streams, then
// convert the hosted event stream onto the client's wire. Replicate
// language-model predictions always stream their output through the stream
// URL, so the handler renders incrementally in every case.
func ChatHandler(c *adaptor.Context, resp *http.Response, requestMeta *meta.Meta) (srvErr *model.ErrorWithStatusCode, usage *model.Usage) {
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return errorWithStatus(errors.Errorf("bad status code [%d]%s", resp.StatusCode, string(payload)), http.StatusInternalServerError), nil
	}
	respBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return errorWithStatus(err, http.StatusInternalServerError), nil
	}
	prediction := new(ChatResponse)
	if err = json.Unmarshal(respBody, prediction); err != nil {
		return errorWithStatus(err, http.StatusInternalServerError), nil
	}

	for {
		if prediction.Status == "failed" || prediction.Status == "canceled" {
			return errorWithStatus(errors.Errorf("prediction %s: %s", prediction.Status, prediction.Error), http.StatusBadGateway), nil
		}
		if prediction.URLs.Stream != "" {
			text, err := streamPredictionOutput(c, requestMeta, prediction.URLs.Stream)
			usage = openai.ResponseText2Usage(text, requestMeta.ActualModelName, requestMeta.PromptTokens)
			if err != nil {
				return errorWithStatus(err, http.StatusBadGateway), usage
			}
			return nil, usage
		}
		if prediction.Status == "succeeded" {
			text := strings.Join(prediction.Output, "")
			usage = openai.ResponseText2Usage(text, requestMeta.ActualModelName, requestMeta.PromptTokens)
			err := wire.Write(c.Writer, requestMeta.Inbound, prediction.ID, requestMeta.ActualModelName, text, wire.Usage{Input: int64(usage.PromptTokens), Output: int64(usage.CompletionTokens)}, requestMeta.IsStream)
			if err != nil {
				return errorWithStatus(err, http.StatusBadGateway), usage
			}
			return nil, usage
		}
		if prediction.URLs.Get == "" {
			return errorWithStatus(errors.New("prediction has neither output nor polling URL"), http.StatusBadGateway), nil
		}
		if err := waitPrediction(c.Request.Context()); err != nil {
			return errorWithStatus(err, http.StatusBadGateway), nil
		}
		prediction, err = pollPrediction(c.Request.Context(), requestMeta.APIKey, prediction.URLs.Get)
		if err != nil {
			return errorWithStatus(err, http.StatusBadGateway), nil
		}
	}
}

func waitPrediction(ctx context.Context) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func pollPrediction(ctx context.Context, apiKey, getURL string) (*ChatResponse, error) {
	taskReq, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "new request")
	}
	taskReq.Header.Set("Authorization", "Bearer "+apiKey)
	taskResp, err := http.DefaultClient.Do(taskReq)
	if err != nil {
		return nil, errors.Wrap(err, "get task")
	}
	defer taskResp.Body.Close()
	if taskResp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(taskResp.Body)
		return nil, errors.Errorf("bad status code [%d]%s", taskResp.StatusCode, string(payload))
	}
	taskBody, err := io.ReadAll(taskResp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read task response")
	}
	taskData := new(ChatResponse)
	if err = json.Unmarshal(taskBody, taskData); err != nil {
		return nil, errors.Wrap(err, "decode task response")
	}
	switch taskData.Status {
	case "succeeded", "failed", "canceled":
		return taskData, nil
	case "":
		return nil, errors.New("prediction status is empty")
	default:
		return taskData, nil
	}
}

// streamPredictionOutput connects the hosted stream URL and converts its
// `event: output` text increments onto the client's wire through the shared
// client-wire session.
func streamPredictionOutput(c *adaptor.Context, requestMeta *meta.Meta, streamURL string) (string, error) {
	streamReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, streamURL, nil)
	if err != nil {
		return "", errors.Wrap(err, "new request to stream")
	}
	streamReq.Header.Set("Authorization", "Bearer "+requestMeta.APIKey)
	streamReq.Header.Set("Accept", "text/event-stream")
	streamReq.Header.Set("Cache-Control", "no-store")
	resp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		return "", errors.Wrap(err, "do request to stream")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		return "", errors.Errorf("bad status code [%d]%s", resp.StatusCode, string(payload))
	}

	var text strings.Builder
	var session *wire.Session
	done := false
	err = sse.Read(resp.Body, func(event sse.Event) error {
		switch event.Name {
		case "output":
			// Replicate output is plain text, not JSON. Preserve quotes and spaces.
			text.WriteString(event.Data)
			if requestMeta.IsStream {
				if session == nil {
					session = wire.NewSession(c.Writer, requestMeta.Inbound, predictionID(requestMeta), requestMeta.ActualModelName)
				}
				return session.Delta(event.Data)
			}
		case "error":
			return errors.Errorf("Replicate stream error: %s", event.Data)
		case "done":
			var result struct {
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(event.Data), &result); err != nil {
				return err
			}
			if result.Reason != "" {
				return errors.Errorf("Replicate prediction ended: %s", result.Reason)
			}
			done = true
			return io.EOF
		}
		return nil
	})
	if err != nil {
		return text.String(), err
	}
	if !done {
		return text.String(), errors.New("Replicate stream ended before done event")
	}
	usage := openai.ResponseText2Usage(text.String(), requestMeta.ActualModelName, requestMeta.PromptTokens)
	report := wire.Usage{Input: int64(usage.PromptTokens), Output: int64(usage.CompletionTokens)}
	if session != nil {
		err = session.Complete(report)
	} else {
		err = wire.Write(c.Writer, requestMeta.Inbound, predictionID(requestMeta), requestMeta.ActualModelName, text.String(), report, requestMeta.IsStream)
	}
	return text.String(), err
}

func predictionID(requestMeta *meta.Meta) string {
	if requestMeta.OriginModelName != "" {
		return "prediction-" + requestMeta.OriginModelName
	}
	return "prediction-provider"
}

func errorWithStatus(err error, status int) *model.ErrorWithStatusCode {
	return &model.ErrorWithStatusCode{
		Error:      model.Error{Message: err.Error()},
		StatusCode: status,
	}
}
