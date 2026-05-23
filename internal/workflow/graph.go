package workflow

import (
	"fmt"
	"sort"
	"strings"
)

const (
	startNodeID = "__start__"
	endNodeID   = "__end__"
)

// GraphMeta holds workflow identity fields for the visualization header, and
// GraphPort / GraphOutput describe inputs and outputs for the graph legend.
type GraphMeta struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type GraphPort struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type GraphOutput struct {
	Name string `json:"name"`
	Expr string `json:"expr,omitempty"`
}

// GraphNode is one vertex in the read-only flowchart IR.
type GraphNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Region string `json:"region,omitempty"`
}

// GraphEdge connects two nodes. Type is sequential|branch|parallel.
type GraphEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

// Graph is the JSON payload returned by graph preview / graph GET endpoints.
type Graph struct {
	Meta    GraphMeta     `json:"meta"`
	Inputs  []GraphPort   `json:"inputs,omitempty"`
	Outputs []GraphOutput `json:"outputs,omitempty"`
	Nodes   []GraphNode   `json:"nodes"`
	Edges   []GraphEdge   `json:"edges"`
}

// GraphFromYAML parses DSL YAML and builds a Graph. Parse errors propagate;
// validation is not required for visualization.
func GraphFromYAML(src string) (*Graph, error) {
	doc, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return GraphFromDoc(doc), nil
}

// GraphFromDoc builds a Graph from an already-parsed WorkflowDoc.
func GraphFromDoc(doc *WorkflowDoc) *Graph {
	if doc == nil {
		return &Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	}
	b := &graphBuilder{}
	preds := []string{startNodeID}
	if len(doc.Triggers) > 0 {
		preds = make([]string, 0, len(doc.Triggers))
		for _, tr := range doc.Triggers {
			nid := "trigger:" + tr.ID
			label := tr.ID
			kind := "trigger"
			detail := ""
			if tr.Cron != "" {
				kind = "trigger-cron"
				detail = tr.Cron
				if tr.Timezone != "" && tr.Timezone != "UTC" {
					detail += " (" + tr.Timezone + ")"
				}
			} else if tr.Webhook != nil {
				kind = "trigger-webhook"
				detail = "webhook"
			}
			b.nodes = append(b.nodes, GraphNode{ID: nid, Kind: kind, Label: label, Detail: detail})
			b.addEdge(startNodeID, nid, "sequential", "trigger")
			preds = append(preds, nid)
		}
	}
	exits := b.runSteps(doc.Steps, preds, "main")
	for _, from := range exits {
		b.addEdge(from, endNodeID, "sequential", "")
	}

	g := &Graph{
		Meta: GraphMeta{
			ID:          doc.ID,
			Name:        doc.Name,
			Description: doc.Description,
		},
		Nodes: append([]GraphNode{
			{ID: startNodeID, Kind: "start", Label: "开始"},
		}, b.nodes...),
		Edges: b.edges,
	}
	g.Nodes = append(g.Nodes, GraphNode{ID: endNodeID, Kind: "end", Label: "结束"})

	if len(doc.Inputs) > 0 {
		names := make([]string, 0, len(doc.Inputs))
		for name := range doc.Inputs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			spec := doc.Inputs[name]
			g.Inputs = append(g.Inputs, GraphPort{Name: name, Type: spec.Type})
		}
	}
	if len(doc.Outputs) > 0 {
		names := make([]string, 0, len(doc.Outputs))
		for name := range doc.Outputs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			g.Outputs = append(g.Outputs, GraphOutput{Name: name, Expr: doc.Outputs[name]})
		}
	}
	return g
}

type graphBuilder struct {
	nodes []GraphNode
	edges []GraphEdge
}

func (b *graphBuilder) addEdge(from, to, typ, label string) {
	if from == "" || to == "" || from == to {
		return
	}
	b.edges = append(b.edges, GraphEdge{From: from, To: to, Type: typ, Label: label})
}

func (b *graphBuilder) runSteps(steps []Step, preds []string, region string) []string {
	exits := preds
	for _, s := range steps {
		exits = b.runStep(s, exits, region)
	}
	return exits
}

func (b *graphBuilder) runStep(s Step, preds []string, region string) []string {
	b.nodes = append(b.nodes, stepGraphNode(s, region))
	for _, from := range preds {
		b.addEdge(from, s.ID, "sequential", "")
	}
	return b.controlExits(s, region)
}

func (b *graphBuilder) controlExits(s Step, region string) []string {
	switch s.Kind {
	case NodeIf:
		return b.ifExits(s, region)
	case NodeForeach:
		return b.foreachExits(s, region)
	case NodeParallel:
		return b.parallelExits(s, region)
	default:
		return []string{s.ID}
	}
}

// runBranchSteps walks a nested step list whose first node is reached from `from`
// via a typed edge (branch / parallel / sequential).
func (b *graphBuilder) runBranchSteps(steps []Step, from, edgeType, edgeLabel, region string) []string {
	if len(steps) == 0 {
		return []string{from}
	}
	exits := []string{from}
	for i, s := range steps {
		if i == 0 {
			b.nodes = append(b.nodes, stepGraphNode(s, region))
			b.addEdge(from, s.ID, edgeType, edgeLabel)
			exits = b.controlExits(s, region)
			continue
		}
		exits = b.runStep(s, exits, region)
	}
	return exits
}

func (b *graphBuilder) ifExits(s Step, region string) []string {
	thenRegion := joinRegion(region, "then")
	elseRegion := joinRegion(region, "else")
	var exits []string
	exits = append(exits, b.runBranchSteps(s.Then, s.ID, "branch", "then", thenRegion)...)
	exits = append(exits, b.runBranchSteps(s.Else, s.ID, "branch", "else", elseRegion)...)
	return exits
}

func (b *graphBuilder) foreachExits(s Step, region string) []string {
	return b.runBranchSteps(s.Steps, s.ID, "sequential", "", joinRegion(region, "foreach"))
}

func (b *graphBuilder) parallelExits(s Step, region string) []string {
	var exits []string
	for i, branch := range s.Parallel {
		subRegion := joinRegion(region, fmt.Sprintf("parallel:%d", i))
		label := fmt.Sprintf("%d", i+1)
		exits = append(exits, b.runBranchSteps(branch, s.ID, "parallel", label, subRegion)...)
	}
	return exits
}

func joinRegion(base, suffix string) string {
	if base == "" {
		return suffix
	}
	return base + "|" + suffix
}

func stepGraphNode(s Step, region string) GraphNode {
	n := GraphNode{ID: s.ID, Kind: string(s.Kind), Region: region}
	switch s.Kind {
	case NodeTool:
		n.Label = s.Use
		n.Detail = truncateDetail(formatArgs(s.Args))
	case NodeAssign:
		n.Label = "assign"
		n.Detail = truncateDetail(strings.Join(sortedKeys(s.Assign), ", "))
	case NodeIf:
		n.Label = "if"
		n.Detail = truncateDetail(s.If)
	case NodeForeach:
		n.Label = "foreach"
		if s.As != "" {
			n.Detail = truncateDetail(fmt.Sprintf("%s as %s", s.Foreach, s.As))
		} else {
			n.Detail = truncateDetail(s.Foreach)
		}
	case NodeParallel:
		n.Label = "parallel"
		n.Detail = fmt.Sprintf("%d branches", len(s.Parallel))
	case NodeWait:
		n.Label = "wait"
		if s.Wait != "" {
			n.Detail = s.Wait
		} else if s.WaitDur > 0 {
			n.Detail = s.WaitDur.String()
		}
	default:
		n.Label = string(s.Kind)
	}
	return n
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, args[k]))
	}
	return strings.Join(parts, ", ")
}

func truncateDetail(s string) string {
	const max = 120
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
