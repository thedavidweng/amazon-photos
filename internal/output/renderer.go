package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/thedavidweng/amazon-photos/internal/model"
	"github.com/thedavidweng/amazon-photos/pkg/aphotos"
)

type RuntimeMetaInput struct {
	Command   string
	Profile   string
	Region    string
	RequestID string
	StartedAt time.Time
}

type Renderer struct {
	Out     io.Writer
	Err     io.Writer
	JSON    bool
	Quiet   bool
	Verbose bool
}

func (r *Renderer) Success(metaInput RuntimeMetaInput, data any, warnings []string) error {
	env := model.Envelope{
		OK:   true,
		Data: data,
		Meta: r.buildMeta(metaInput, warnings),
	}
	return r.writeJSON(&env)
}

func (r *Renderer) Failure(metaInput RuntimeMetaInput, errBody model.ErrorBody, warnings []string) error {
	errBody.Message = aphotos.Redact(errBody.Message)
	if errBody.Category == "" {
		errBody.Category = model.CategoryForCode(errBody.Code)
	}
	if !r.JSON {
		if r.Err != nil {
			_, _ = fmt.Fprintf(r.Err, "Error [%s]: %s\n", errBody.Code, errBody.Message)
		}
		return &model.CommandError{
			Code:    errBody.Code,
			Message: errBody.Message,
			Body:    errBody,
		}
	}
	env := model.Envelope{
		OK:    false,
		Error: &errBody,
		Meta:  r.buildMeta(metaInput, warnings),
	}
	_ = r.writeJSON(&env)
	return &model.CommandError{
		Code:    errBody.Code,
		Message: errBody.Message,
		Body:    errBody,
	}
}

func (r *Renderer) FailError(metaInput RuntimeMetaInput, err error) error {
	cmdErr := model.FromError(err)
	if cmdErr == nil {
		return nil
	}
	if metaInput.RequestID == "" {
		if reqID, ok := cmdErr.Body.Details["request_id"].(string); ok {
			metaInput.RequestID = reqID
		}
	}
	var warnings []string
	if w, ok := cmdErr.Body.Details["warnings"].([]string); ok {
		warnings = w
	}
	err2 := r.Failure(metaInput, cmdErr.Body, warnings)
	if ce, ok := err2.(*model.CommandError); ok {
		ce.Cause = cmdErr.Cause
		return ce
	}
	return err2
}

func (r *Renderer) PartialFailure(metaInput RuntimeMetaInput, data any, err error, warnings []string) error {
	cmdErr := model.FromError(err)
	errBody := model.ErrorBody{
		Code:     model.ErrPartialSuccess,
		Category: model.CategoryForCode(model.ErrPartialSuccess),
	}
	if cmdErr != nil {
		errBody = cmdErr.Body
		errBody.Code = model.ErrPartialSuccess
		errBody.Category = model.CategoryForCode(model.ErrPartialSuccess)
	}
	if errBody.Message == "" && err != nil {
		errBody.Message = aphotos.Redact(err.Error())
	}
	if !r.JSON {
		if r.Err != nil {
			_, _ = fmt.Fprintf(r.Err, "Error [%s]: %s\n", errBody.Code, errBody.Message)
		}
		return &model.CommandError{
			Code:    model.ErrPartialSuccess,
			Message: errBody.Message,
			Body:    errBody,
			Cause:   err,
		}
	}
	env := model.Envelope{
		OK:    false,
		Data:  data,
		Error: &errBody,
		Meta:  r.buildMeta(metaInput, warnings),
	}
	_ = r.writeJSON(&env)
	return &model.CommandError{
		Code:    model.ErrPartialSuccess,
		Message: errBody.Message,
		Body:    errBody,
		Cause:   err,
	}
}

func (r *Renderer) FailWithData(metaInput RuntimeMetaInput, data any, err error, warnings []string) error {
	cmdErr := model.FromError(err)
	if cmdErr == nil {
		return nil
	}
	if !r.JSON {
		if r.Err != nil {
			_, _ = fmt.Fprintf(r.Err, "Error [%s]: %s\n", cmdErr.Code, cmdErr.Error())
		}
		return cmdErr
	}
	env := model.Envelope{
		OK:    false,
		Data:  data,
		Error: &cmdErr.Body,
		Meta:  r.buildMeta(metaInput, warnings),
	}
	_ = r.writeJSON(&env)
	return cmdErr
}

func (r *Renderer) Render(metaInput RuntimeMetaInput, data any, warnings []string, human func()) error {
	if r.JSON {
		return r.Success(metaInput, data, warnings)
	}
	if !r.Quiet && human != nil {
		human()
	}
	return nil
}

func (r *Renderer) Human(format string, args ...any) {
	if r.JSON || r.Quiet || r.Out == nil {
		return
	}
	msg := aphotos.Redact(fmt.Sprintf(format, args...))
	_, _ = fmt.Fprintln(r.Out, msg)
}

func (r *Renderer) Progressf(format string, args ...any) {
	if r.Quiet || r.Err == nil {
		return
	}
	msg := aphotos.Redact(fmt.Sprintf(format, args...))
	_, _ = fmt.Fprintln(r.Err, msg)
}

func (r *Renderer) Verbosef(format string, args ...any) {
	if !r.Verbose || r.Quiet || r.Err == nil {
		return
	}
	msg := aphotos.Redact(fmt.Sprintf(format, args...))
	_, _ = fmt.Fprintln(r.Err, msg)
}

func (r *Renderer) buildMeta(input RuntimeMetaInput, warnings []string) model.Meta {
	var dur int64
	if !input.StartedAt.IsZero() {
		dur = max(time.Since(input.StartedAt).Milliseconds(), 0)
	}
	var cleanWarnings []string
	if len(warnings) > 0 {
		cleanWarnings = make([]string, len(warnings))
		for i, w := range warnings {
			cleanWarnings[i] = aphotos.Redact(w)
		}
	}
	return model.Meta{
		Command:       aphotos.Redact(input.Command),
		Profile:       aphotos.Redact(input.Profile),
		Region:        aphotos.Redact(input.Region),
		DurationMS:    dur,
		SchemaVersion: model.SchemaVersion,
		RequestID:     aphotos.Redact(input.RequestID),
		Warnings:      cleanWarnings,
	}
}

func (r *Renderer) writeJSON(env *model.Envelope) error {
	if r.Out == nil {
		return nil
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	redacted := aphotos.Redact(string(raw))
	_, err = io.WriteString(r.Out, redacted+"\n")
	return err
}
