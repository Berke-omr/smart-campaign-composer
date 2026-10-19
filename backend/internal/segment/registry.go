// Package segment is the audience engine: a small JSON rule language (DSL)
// over whitelisted user fields, its validator, and a compiler to
// parameterized SQL.
//
// The LLM never writes SQL. It emits a Rule; Validate turns a Rule into a
// Validated value, and Compile accepts nothing else. Column names in the SQL
// come only from the registry below; values only ever travel as bind
// parameters. See DECISIONS.md D9.
package segment

// FieldType decides which operators a field accepts and how values are parsed.
type FieldType string

const (
	TypeEnum         FieldType = "enum"          // closed set of strings
	TypeRelativeTime FieldType = "relative_time" // timestamp compared with "N days before now"
	TypeInteger      FieldType = "integer"
	TypeNumber       FieldType = "number"
)

// Op is a comparison operator in the DSL.
type Op string

const (
	OpEq                Op = "eq"
	OpNeq               Op = "neq"
	OpIn                Op = "in"
	OpNotIn             Op = "not_in"
	OpGt                Op = "gt"
	OpGte               Op = "gte"
	OpLt                Op = "lt"
	OpLte               Op = "lte"
	OpBetween           Op = "between"
	OpWithinLastDays    Op = "within_last_days"
	OpNotWithinLastDays Op = "not_within_last_days"
	OpIsNull            Op = "is_null"
	OpIsNotNull         Op = "is_not_null"
)

// valueShape is the JSON shape an operator expects as its value.
type valueShape int

const (
	shapeScalar valueShape = iota // one string or number
	shapeList                     // array of strings
	shapePair                     // [min, max]
	shapeDays                     // whole number of days
	shapeNone                     // no value (null or omitted)
)

func (op Op) shape() valueShape {
	switch op {
	case OpIn, OpNotIn:
		return shapeList
	case OpBetween:
		return shapePair
	case OpWithinLastDays, OpNotWithinLastDays:
		return shapeDays
	case OpIsNull, OpIsNotNull:
		return shapeNone
	default:
		return shapeScalar
	}
}

// Limits that keep rules small and queries cheap.
const (
	MaxConditions = 10
	MaxListValues = 20
	MinDays       = 1
	MaxDays       = 730
)

// Field describes one targetable user attribute.
type Field struct {
	Name        string    `json:"name"`  // DSL name, e.g. "last_cart_add_at"
	Label       string    `json:"label"` // human label for descriptions and the UI
	Description string    `json:"description"`
	Type        FieldType `json:"type"`
	Ops         []Op      `json:"ops"`
	Nullable    bool      `json:"nullable"`
	Enum        []string  `json:"enum,omitempty"`
	Min         float64   `json:"min,omitempty"` // numeric fields only
	Max         float64   `json:"max,omitempty"`

	// column is the SQL column. It is a trusted constant and never derived
	// from input; it is not exposed in JSON.
	column string
}

func (f *Field) allows(op Op) bool {
	for _, o := range f.Ops {
		if o == op {
			return true
		}
	}
	return false
}

// Registry is the catalog of targetable fields: the single source of truth
// for the validator, the compiler, the LLM's JSON schema and the UI.
type Registry struct {
	fields []Field
	byName map[string]*Field
}

var (
	enumOps         = []Op{OpEq, OpNeq, OpIn, OpNotIn}
	relativeTimeOps = []Op{OpWithinLastDays, OpNotWithinLastDays, OpIsNull, OpIsNotNull}
	integerOps      = []Op{OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte, OpBetween}
	numberOps       = []Op{OpGt, OpGte, OpLt, OpLte, OpBetween}
)

// NewRegistry builds the catalog. cities are the values present in the data
// (read from the database at startup); with none, the city field is left out
// because an empty enum could never match.
//
// Deliberately absent: names, email and phone (personal data, useless for
// targeting) and the consent flags (the backend always applies consent itself,
// so a rule must not be able to switch it off).
func NewRegistry(cities []string) *Registry {
	var fields []Field
	if len(cities) > 0 {
		fields = append(fields, Field{
			Name: "city", column: "city", Label: "City", Type: TypeEnum, Ops: enumOps,
			Enum:        append([]string(nil), cities...),
			Description: "City the user lives in. Values are exact, with Turkish characters (e.g. \"İstanbul\").",
		})
	}
	fields = append(fields,
		Field{
			Name: "language", column: "language", Label: "Language", Type: TypeEnum, Ops: []Op{OpEq, OpNeq},
			Enum:        []string{"tr", "en"},
			Description: "Preferred language of the user: \"tr\" (Turkish) or \"en\" (English).",
		},
		Field{
			Name: "last_cart_add_at", column: "last_cart_add_at", Label: "Last added to cart",
			Type: TypeRelativeTime, Ops: relativeTimeOps, Nullable: true,
			Description: "When the user last added a product to the cart. Null if they never did.",
		},
		Field{
			Name: "last_purchase_at", column: "last_purchase_at", Label: "Last purchase",
			Type: TypeRelativeTime, Ops: relativeTimeOps, Nullable: true,
			Description: "When the user last completed a purchase. Null if they never purchased.",
		},
		Field{
			Name: "created_at", column: "created_at", Label: "Signed up",
			Type: TypeRelativeTime, Ops: []Op{OpWithinLastDays, OpNotWithinLastDays},
			Description: "When the user signed up.",
		},
		Field{
			Name: "order_count", column: "order_count", Label: "Order count",
			Type: TypeInteger, Ops: integerOps, Min: 0, Max: 1000,
			Description: "Number of orders the user has placed (0 = never purchased).",
		},
		Field{
			Name: "total_spent", column: "total_spent", Label: "Total spent (TRY)",
			Type: TypeNumber, Ops: numberOps, Min: 0, Max: 10_000_000,
			Description: "Lifetime spend of the user in Turkish lira.",
		},
	)

	r := &Registry{fields: fields, byName: make(map[string]*Field, len(fields))}
	for i := range r.fields {
		r.byName[r.fields[i].Name] = &r.fields[i]
	}
	return r
}

// Field looks a field up by its DSL name.
func (r *Registry) Field(name string) (*Field, bool) {
	f, ok := r.byName[name]
	return f, ok
}

// Fields returns a copy of all fields, in a stable order.
func (r *Registry) Fields() []Field {
	return append([]Field(nil), r.fields...)
}

// FieldNames lists the DSL names of all fields.
func (r *Registry) FieldNames() []string {
	names := make([]string, len(r.fields))
	for i, f := range r.fields {
		names[i] = f.Name
	}
	return names
}
