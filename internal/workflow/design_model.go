package workflow

// WorkflowDesign is the Slice 20 visual editor model (JSON); CompileDesign → dsl_yaml.
type WorkflowDesign struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Inputs      []InputField  `json:"inputs,omitempty"`
	Steps       []DesignStep  `json:"steps"`
	Outputs     []OutputField `json:"outputs,omitempty"`
}

// InputField describes one workflow input for forms and compile.
type InputField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Default     any      `json:"default,omitempty"`
	Label       string   `json:"label,omitempty"`
	Description string   `json:"description,omitempty"`
	Widget      string   `json:"widget,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// OutputField binds one workflow output.
type OutputField struct {
	Name string `json:"name"`
	Expr string `json:"expr"`
}

// DesignStep is one step (tool / assign / if).
type DesignStep struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`

	Tool string     `json:"tool,omitempty"`
	Args []ArgField `json:"args,omitempty"`

	Assignments []AssignField `json:"assignments,omitempty"`

	Condition *DesignCondition `json:"condition,omitempty"`
	Then      []DesignStep     `json:"then,omitempty"`
	Else      []DesignStep     `json:"else,omitempty"`
}

// ArgField is one tool argument.
type ArgField struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	ValueKind string `json:"valueKind"`
}

// AssignField sets vars.<name>.
type AssignField struct {
	Var   string `json:"var"`
	Expr  string `json:"expr"`
	Label string `json:"label,omitempty"`
}

// DesignCondition compiles to a workflow if expression.
type DesignCondition struct {
	Left      string `json:"left"`
	LeftKind  string `json:"leftKind,omitempty"`
	Op        string `json:"op"`
	Right     string `json:"right"`
	RightKind string `json:"rightKind,omitempty"`
}

// DesignCompileResult is returned by CompileDesign.
type DesignCompileResult struct {
	DSLYAML  string   `json:"dsl_yaml"`
	Warnings []string `json:"warnings,omitempty"`
}

// DesignDecompileResult is returned by DecompileDesign.
type DesignDecompileResult struct {
	Design   *WorkflowDesign `json:"design"`
	Warnings []string        `json:"warnings,omitempty"`
}
