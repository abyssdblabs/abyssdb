// Package render pretty-prints AbyssDB results in a psql-inspired format,
// which is the only part of the user experience we fully control.
package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/noctarius/abyssdb/internal/engine"
)

func Result(w io.Writer, r *engine.Result) {
	for _, n := range r.AllNotices() {
		fmt.Fprintf(w, "NOTICE:  %s\n", n)
	}
	if r.Columns != nil || strings.HasPrefix(r.Tag, "SELECT") {
		if len(r.Columns) == 0 {
			fmt.Fprintln(w, "--")
		} else {
			fmt.Fprintln(w, " "+strings.Join(r.Columns, " | "))
			seps := make([]string, len(r.Columns))
			for i, c := range r.Columns {
				seps[i] = strings.Repeat("-", len(c)+2)
			}
			fmt.Fprintln(w, strings.Join(seps, "+"))
		}
		fmt.Fprintf(w, "(%d rows)\n", len(r.Rows))
	} else {
		fmt.Fprintln(w, r.Tag)
	}
	fmt.Fprintln(w, "Time: 0.000 ms")
}
