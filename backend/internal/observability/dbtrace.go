package observability

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudster1/rotor/backend/internal/config"
)

// QueryTracer is a pgx tracer that draws a span per statement, under the caller's span only: the worker's polling
// would otherwise be a stream of roots nobody asked for. Statements carry no arguments, so no value reaches a span.
func QueryTracer() pgx.QueryTracer { return queryTracer{} }

type queryTracer struct{}

type queryKey struct{}

func (queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.SpanFromContext(ctx).IsRecording() {
		return ctx
	}
	statement := data.SQL
	if len(statement) > config.MaxSpanAttrChars {
		statement = statement[:config.MaxSpanAttrChars]
	}
	op := firstWord(data.SQL)
	ctx, span := Tracer().Start(ctx, "db."+op, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		semconv.DBSystemNamePostgreSQL,
		semconv.DBOperationName(op),
		semconv.DBQueryText(statement),
	))
	return context.WithValue(ctx, queryKey{}, span)
}

func (queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(queryKey{}).(trace.Span)
	if !ok {
		return
	}
	// No rows is an answer, not a failure.
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}
	span.End()
}

// firstWord names a statement by its verb, lowercased, or "other".
func firstWord(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "other"
	}
	switch op := strings.ToLower(fields[0]); op {
	case "select", "insert", "update", "delete", "with", "begin", "commit", "rollback":
		return op
	}
	return "other"
}
