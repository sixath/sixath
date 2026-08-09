package executor

import (
	"context"
	"errors"
	"time"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/obs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var execTracer = otel.Tracer("github.com/sixath/framework/executor")

// ErrorKind 为 Prometheus executor_errors_total 的有限标签枚举。
const (
	ErrorKindSchema      = "schema"
	ErrorKindReadonly    = "readonly"
	ErrorKindTimeout     = "timeout"
	ErrorKindDriver      = "driver"
	ErrorKindUnsupported = "unsupported"
	ErrorKindOther       = "other"
)

type opRecorder struct {
	span       trace.Span
	start      time.Time
	datasource string
	dsType     string
	op         string
	maxRows    int
	allowWrite bool
}

func lookupDSType(reg *datasource.Registry, datasourceID string) string {
	if reg == nil {
		return "unknown"
	}
	ds, err := reg.Get(datasourceID)
	if err != nil {
		return "unknown"
	}
	return ds.Type()
}

func beginOp(ctx context.Context, reg *datasource.Registry, datasourceID, op string, maxRows int, allowWrite bool) (context.Context, *opRecorder) {
	dsType := lookupDSType(reg, datasourceID)
	attrs := []attribute.KeyValue{
		attribute.String("db.system", dsType),
		attribute.String("db.datasource_id", datasourceID),
		attribute.String("executor.op", op),
		attribute.Int("executor.max_rows", maxRows),
		attribute.Bool("executor.allow_write", allowWrite),
	}
	ctx, span := execTracer.Start(ctx, "Executor."+op,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx, &opRecorder{
		span:       span,
		start:      time.Now(),
		datasource: datasourceID,
		dsType:     dsType,
		op:         op,
		maxRows:    maxRows,
		allowWrite: allowWrite,
	}
}

func (r *opRecorder) finish(res *Result, err error) {
	if r == nil {
		return
	}
	defer r.span.End()

	status := "ok"
	errKind := ""
	switch {
	case err != nil && errors.Is(err, ErrReadOnlyViolation):
		status = "rejected"
		errKind = ErrorKindReadonly
		r.span.RecordError(err)
		r.span.SetStatus(codes.Ok, "readonly_reject")
	case err != nil:
		status = "error"
		errKind = ClassifyErrorKind(err)
		r.span.RecordError(err)
		r.span.SetStatus(codes.Error, err.Error())
	default:
		r.span.SetStatus(codes.Ok, "")
	}

	rows := 0
	truncated := false
	if res != nil {
		rows = len(res.Rows)
		truncated = res.Truncated
		r.span.SetAttributes(
			attribute.Int("executor.rows_returned", rows),
			attribute.Bool("executor.truncated", truncated),
			attribute.Int64("executor.affected_rows", res.AffectedRows),
		)
	}

	obs.ObserveExecutorRun(r.datasource, r.dsType, r.op, status, time.Since(r.start), rows, errKind)
}

// ClassifyErrorKind 将 error 映射为有限 error_kind 标签。
func ClassifyErrorKind(err error) string {
	if err == nil {
		return ""
	}
	if IsSchemaRelated(err) {
		return ErrorKindSchema
	}
	if errors.Is(err, ErrReadOnlyViolation) {
		return ErrorKindReadonly
	}
	if errors.Is(err, ErrUnsupportedDataSource) || errors.Is(err, ErrUnsupportedSyntax) {
		return ErrorKindUnsupported
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrorKindTimeout
	}
	return ErrorKindDriver
}
