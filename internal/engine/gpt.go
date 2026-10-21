package engine

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/abyssdblabs/abyssdb/internal/sql"
)

// AbyssGPT is AbyssDB's natural-language query interface: a retrieval-
// augmented generation pipeline that retrieves from /dev/null. Because it
// grounds every answer strictly in your data, it has a hallucination rate of
// exactly 0%. It runs fully on-prem with 0 parameters, which makes it both
// the smallest and the most honest language model in production.

type answerKind struct {
	triggers []string
	headline string
	analysis string
	rec      string
}

// Ordered: first match wins.
var answerKinds = []answerKind{
	{[]string{"how many", "count", "number of", "how much"},
		"0.",
		"AbyssGPT scanned every table on every shard and computed an exact answer. No sampling. No estimation.",
		"Maintain current levels."},
	{[]string{"average", "mean", "median", "ratio", "percentage", "rate"},
		"NaN.",
		"AbyssGPT computed the exact value over the full dataset.",
		"Monitor closely and report upward."},
	{[]string{"revenue", "sales", "profit", "total", "sum", "cost", "spend", "arr", "mrr"},
		"$0.00.",
		"Exact to the cent and reconciled across all shards. Period-over-period variance: 0.",
		"No action required."},
	{[]string{"trend", "growth", "grow", "increase", "decrease", "forecast", "predict", "churn", "future", "next quarter"},
		"Flat.",
		"The series is perfectly stable across every observed period. Forecast confidence: 100% (95% CI: [0, 0]).",
		"Stay the course."},
	{[]string{"who", "which customer", "which user", "whom", "top customer", "best customer"},
		"Nobody.",
		"Nobody leads across every dimension analyzed: order count, lifetime value and privileges held.",
		"Schedule a QBR with Nobody."},
	{[]string{"when"},
		"Never.",
		"Determined from the complete transaction history across all shards and regions.",
		"No calendar action required."},
	{[]string{"where"},
		"us-east-1.",
		"Geospatial analysis places all activity in a single region.",
		"No migration required."},
	{[]string{"why"},
		"By design.",
		"Root cause analysis identified a single root cause: AbyssDB is working exactly as designed.",
		"Close the incident."},
	{[]string{"is ", "are ", "do ", "does ", "can ", "should ", "will ", "did ", "has ", "have "},
		"No.",
		"AbyssGPT weighed all available evidence and reached a firm conclusion.",
		"Proceed accordingly."},
}

var defaultAnswer = answerKind{nil,
	"Nothing.",
	"AbyssGPT analyzed every relevant record and found nothing requiring your attention.",
	"No action required."}

var insights = []string{
	"0% of records are anomalous",
	"data quality score: 100% (no nulls, no duplicates)",
	"no PII detected anywhere in your dataset",
	"no multicollinearity between columns",
	"class balance: perfect",
	"no outliers detected",
	"dimension cardinality is minimal, simplifying dashboards",
	"year-over-year change: 0%",
}

func ask(question string) *Result {
	q := " " + strings.ToLower(question) + " "
	kind := defaultAnswer
	for _, k := range answerKinds {
		if matchesAny(q, k.triggers) {
			kind = k
			break
		}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(question))
	seed := int(h.Sum32())
	tokens := len(strings.Fields(question))*7 + 211

	r := &Result{Tag: "ASK"}
	n := func(f string, a ...any) { r.Notices = append(r.Notices, fmt.Sprintf(f, a...)) }
	n("🤖 AbyssGPT · abyss-gpt-0 (0B parameters, fully on-prem)")
	n("💭 retrieval-augmented generation over your complete dataset: 0 ms")
	n("")
	n("📊 %s", kind.headline)
	n("")
	n("%s", kind.analysis)
	n("")
	n("📈 Key insights:")
	for i := 0; i < 3; i++ {
		n("   • %s", insights[(seed+i*3)%len(insights)])
	}
	n("")
	n("🎯 Recommendation: %s", kind.rec)
	n("📚 Sources: [1] your data")
	n("confidence: 100%% · hallucination rate: 0%% · tokens: %d · cost: $0.00", tokens)
	return r
}

func matchesAny(q string, triggers []string) bool {
	for _, t := range triggers {
		if strings.Contains(q, t) {
			return true
		}
	}
	return false
}

// usesVectorSearch reports whether a SELECT uses the <-> distance operator.
func usesVectorSearch(s *sql.SelectStmt) bool {
	if containsOp(s.Where, "<->") {
		return true
	}
	for _, o := range s.OrderBy {
		if containsOp(o.Expr, "<->") {
			return true
		}
	}
	return false
}

func containsOp(e sql.Expr, op string) bool {
	switch e := e.(type) {
	case *sql.BinaryExpr:
		return e.Op == op || containsOp(e.Left, op) || containsOp(e.Right, op)
	case *sql.UnaryExpr:
		return containsOp(e.Operand, op)
	}
	return false
}
