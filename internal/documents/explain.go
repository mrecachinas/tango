package documents

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"tango/internal/advisor"
	"tango/internal/state"
)

// Explain runs explain for op in db and shows the analysis in the detail view.
// The server stops the explain at twice the query timeout and still returns
// the partial plan, so slow queries are explained rather than abandoned.
func Explain(db string, op advisor.Op, verbosity, queryText string) {
	s := state.S
	client := s.Client
	timeout := s.QueryTimeout
	if timeout <= 0 {
		timeout = defaultExplainTimeout
	}
	from := s.View
	state.AsyncTimeout("Explaining "+op.Kind, 3*timeout, func(ctx context.Context) func() {
		database := client.Database(db)
		var raw bson.D
		err := database.RunCommand(ctx, op.ExplainCommand(verbosity, (2*timeout).Milliseconds())).Decode(&raw)
		var specs []bson.D
		if err == nil && op.Coll != "" {
			if cur, e := database.Collection(op.Coll).Indexes().List(ctx); e == nil {
				_ = cur.All(ctx, &specs)
			}
		}
		return func() {
			if err != nil {
				state.ShowError("explain failed: " + err.Error())
				return
			}
			plan, err := advisor.ParseExplain(raw)
			if err != nil {
				state.ShowError("can't read explain output: " + err.Error())
				return
			}
			analysis := advisor.Assess(plan, &op, specs)
			title := "Explain · " + advisor.CollRef(op.Coll)
			state.SetDetail(title, analysis.Report(verbosity, queryText, raw))
			if from == "insights" {
				s.DetailBackPage = "insights"
			}
		}
	})
}

const defaultExplainTimeout = 30 * time.Second // when no query timeout is set

// ExplainInsightQuery explains query shape id ("Q3") from the insights view.
func ExplainInsightQuery(id, verbosity string) error {
	s := state.S
	q, ok := s.InsightsQueries[id]
	if !ok {
		if len(s.InsightsQueries) == 0 {
			return fmt.Errorf("no insights loaded — run :insights first")
		}
		return fmt.Errorf("unknown query %s — :insights lists Q1…Q%d", id, len(s.InsightsQueries))
	}
	op, ok := advisor.OpFromCommand(q.Command)
	if !ok {
		return fmt.Errorf("%s can't be explained (%s)", id, q.Shape)
	}
	Explain(q.NS.DB, op, verbosity, id+" · "+q.Shape)
	return nil
}
