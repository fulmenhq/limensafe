package data

// Synthetic path-only leak fixture — see corpus/synthetic-acme/README.md.
// File content is deliberately clean; the leak is ONLY in the path segment.
// V0 spike test T4: scanner detects the path-segment leak and emits a
// finding whose location.path does NOT echo the protected substring.

type Record struct {
	ID    string
	Value string
}

func New(id, value string) *Record {
	return &Record{ID: id, Value: value}
}

func (r *Record) IsValid() bool {
	return r.ID != "" && r.Value != ""
}
