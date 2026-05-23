package workflow_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow"
)

func TestGraphFromYAML_Linear(t *testing.T) {
	g, err := workflow.GraphFromYAML(svcDSL)
	require.NoError(t, err)
	require.Equal(t, "greet", g.Meta.ID)
	require.Len(t, g.Nodes, 3) // start, build, end
	require.Equal(t, "__start__", g.Nodes[0].ID)
	require.Equal(t, "build", g.Nodes[1].ID)
	require.Equal(t, "assign", g.Nodes[1].Kind)
	require.Equal(t, "__end__", g.Nodes[2].ID)

	var startToBuild bool
	for _, e := range g.Edges {
		if e.From == "__start__" && e.To == "build" {
			startToBuild = true
		}
	}
	require.True(t, startToBuild, "start → build edge")
}

func TestGraphFromYAML_IfBranches(t *testing.T) {
	src := `
id: cond
name: Cond
steps:
  - id: gate
    if: ${vars.ok}
    then:
      - id: yes
        assign:
          path: yes
    else:
      - id: no
        assign:
          path: no
  - id: after
    wait: 1ms
`
	g, err := workflow.GraphFromYAML(src)
	require.NoError(t, err)

	nodeIDs := map[string]workflow.GraphNode{}
	for _, n := range g.Nodes {
		nodeIDs[n.ID] = n
	}
	require.Contains(t, nodeIDs, "gate")
	require.Equal(t, "if", nodeIDs["gate"].Kind)
	require.Contains(t, nodeIDs, "yes")
	require.Contains(t, nodeIDs, "no")
	require.Contains(t, nodeIDs, "after")

	var thenEdge, elseEdge, yesToAfter, noToAfter bool
	for _, e := range g.Edges {
		switch {
		case e.From == "gate" && e.To == "yes" && e.Type == "branch" && e.Label == "then":
			thenEdge = true
		case e.From == "gate" && e.To == "no" && e.Type == "branch" && e.Label == "else":
			elseEdge = true
		case e.From == "yes" && e.To == "after":
			yesToAfter = true
		case e.From == "no" && e.To == "after":
			noToAfter = true
		}
	}
	require.True(t, thenEdge)
	require.True(t, elseEdge)
	require.True(t, yesToAfter)
	require.True(t, noToAfter)
}

func TestGraphFromYAML_Parallel(t *testing.T) {
	src := `
id: par
name: Par
steps:
  - id: fan
    parallel:
      - - id: a1
          use: alpha
      - - id: b1
          use: beta
  - id: done
    assign:
      ok: "1"
`
	g, err := workflow.GraphFromYAML(src)
	require.NoError(t, err)

	var parA, parB, aDone, bDone bool
	for _, e := range g.Edges {
		switch {
		case e.From == "fan" && e.To == "a1" && e.Type == "parallel":
			parA = true
		case e.From == "fan" && e.To == "b1" && e.Type == "parallel":
			parB = true
		case e.From == "a1" && e.To == "done":
			aDone = true
		case e.From == "b1" && e.To == "done":
			bDone = true
		}
	}
	require.True(t, parA)
	require.True(t, parB)
	require.True(t, aDone)
	require.True(t, bDone)
}

func TestGraphFromYAML_Triggers(t *testing.T) {
	src := `
id: trig
name: Trig
triggers:
  - id: schedule
    cron: "0 9 * * *"
  - id: inbound
    webhook: {}
steps:
  - id: a
    wait: 1ms
`
	g, err := workflow.GraphFromYAML(src)
	require.NoError(t, err)
	nodeIDs := map[string]bool{}
	for _, n := range g.Nodes {
		nodeIDs[n.ID] = true
	}
	require.True(t, nodeIDs["trigger:schedule"])
	require.True(t, nodeIDs["trigger:inbound"])
	var startCron, cronStep bool
	for _, e := range g.Edges {
		if e.From == "__start__" && e.To == "trigger:schedule" {
			startCron = true
		}
		if e.From == "trigger:schedule" && e.To == "a" {
			cronStep = true
		}
	}
	require.True(t, startCron)
	require.True(t, cronStep)
}

func TestGraphFromYAML_InvalidYAML(t *testing.T) {
	_, err := workflow.GraphFromYAML("not: [valid")
	require.Error(t, err)
}
