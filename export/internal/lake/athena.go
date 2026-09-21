package lake

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// AthenaRunner runs SQL statements against a fixed database, polling until each one finishes.
// WorkGroup should always be set explicitly (never left to default to "primary") - found the hard
// way (Phase 3) that leaving it unset let a query run against a stale cached Iceberg snapshot
// (undercounting a live table by 11 rows, reproducible, unrelated to Athena's query-result-reuse
// feature which was confirmed off) while an explicit workgroup consistently saw the correct
// current data. OutputLocation is still passed explicitly on every query rather than relying on
// the workgroup's own configured result location, since different callers write to different
// prefixes.
type AthenaRunner struct {
	Client         *athena.Client
	Database       string
	OutputLocation string
	WorkGroup      string
}

func (r *AthenaRunner) Run(ctx context.Context, sql string) (*athena.GetQueryExecutionOutput, error) {
	input := &athena.StartQueryExecutionInput{
		QueryString:           aws.String(sql),
		QueryExecutionContext: &types.QueryExecutionContext{Database: aws.String(r.Database)},
		ResultConfiguration:   &types.ResultConfiguration{OutputLocation: aws.String(r.OutputLocation)},
	}
	if r.WorkGroup != "" {
		input.WorkGroup = aws.String(r.WorkGroup)
	}
	start, err := r.Client.StartQueryExecution(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("start query execution: %w\nsql: %s", err, sql)
	}

	id := *start.QueryExecutionId
	for {
		out, err := r.Client.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{QueryExecutionId: aws.String(id)})
		if err != nil {
			return nil, fmt.Errorf("get query execution %s: %w", id, err)
		}

		switch out.QueryExecution.Status.State {
		case types.QueryExecutionStateSucceeded:
			return out, nil
		case types.QueryExecutionStateFailed, types.QueryExecutionStateCancelled:
			reason := "(no reason given)"
			if out.QueryExecution.Status.StateChangeReason != nil {
				reason = *out.QueryExecution.Status.StateChangeReason
			}
			return out, fmt.Errorf("query %s ended %s: %s\nsql: %s", id, out.QueryExecution.Status.State, reason, sql)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// RunAndFetchSingleValue runs a query expected to return exactly one row with one column
// (a COUNT(*) or SUM(...) style aggregate) and returns that value as a string.
func (r *AthenaRunner) RunAndFetchSingleValue(ctx context.Context, sql string) (string, error) {
	out, err := r.Run(ctx, sql)
	if err != nil {
		return "", err
	}

	res, err := r.Client.GetQueryResults(ctx, &athena.GetQueryResultsInput{
		QueryExecutionId: out.QueryExecution.QueryExecutionId,
	})
	if err != nil {
		return "", fmt.Errorf("get query results: %w", err)
	}

	rows := res.ResultSet.Rows
	// Athena's result set includes a header row for the column name(s).
	if len(rows) < 2 || len(rows[1].Data) < 1 || rows[1].Data[0].VarCharValue == nil {
		return "", fmt.Errorf("query returned no usable result row: %s", sql)
	}
	return *rows[1].Data[0].VarCharValue, nil
}
