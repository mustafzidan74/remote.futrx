package schedule

import (
	"fmt"
	"sort"
	"strings"
)

// MaxChainDepth bounds how many tasks one settled run may pull behind it.
// It is enforced twice: at save time (a declared chain longer than this is
// rejected) and at fire time (a run whose chain position already reached the
// bound triggers nothing further), so a chain edited into a longer shape while
// runs are in flight still cannot run away.
const MaxChainDepth = 10

// MaxChainLinks bounds the fan-out of a single task.
const MaxChainLinks = 10

// ChainWhen selects which settled outcomes trigger a chained task.
type ChainWhen string

const (
	ChainWhenSuccess ChainWhen = "success"
	ChainWhenFailure ChainWhen = "failure"
	ChainWhenAlways  ChainWhen = "always"
)

// ChainLink is one "then run…" edge of a task.
type ChainLink struct {
	TaskID ID        `json:"taskId"`
	When   ChainWhen `json:"when"`
	// DelayMin postpones the triggered one-off run by this many minutes.
	DelayMin int `json:"delayMin,omitempty"`
}

// ChainRun is the position of a triggered run inside its chain. Depth is
// 1-based: the run that started the chain is 1, the first task it triggered
// is 2. Total is the longest path reachable from the chain's root, so the
// notification can read "chain 2/3".
type ChainRun struct {
	FromTaskID ID  `json:"fromTaskId,omitempty"`
	Depth      int `json:"depth,omitempty"`
	Total      int `json:"total,omitempty"`
}

// Label renders the human-facing "chain 2/3" fragment.
func (c *ChainRun) Label() string {
	if c == nil || c.Depth <= 0 {
		return ""
	}
	total := c.Total
	if total < c.Depth {
		total = c.Depth
	}
	return fmt.Sprintf("chain %d/%d", c.Depth, total)
}

func normalizeChain(links []ChainLink) []ChainLink {
	if len(links) == 0 {
		return nil
	}
	normalized := make([]ChainLink, 0, len(links))
	seen := make(map[ID]struct{}, len(links))
	for _, link := range links {
		link.TaskID = ID(strings.TrimSpace(string(link.TaskID)))
		if link.TaskID == "" {
			continue
		}
		if _, duplicate := seen[link.TaskID]; duplicate {
			continue
		}
		seen[link.TaskID] = struct{}{}
		if link.When == "" {
			link.When = ChainWhenSuccess
		}
		if link.DelayMin < 0 {
			link.DelayMin = 0
		}
		normalized = append(normalized, link)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// validateChain checks the edges of one task against the rest of the catalog:
// every target must exist, live in the same project, and never lead back to
// the task being saved. Depth is bounded so a save can never define work the
// fire path would have to truncate.
func validateChain(task Task, catalog []Task) error {
	if len(task.Next) == 0 {
		return nil
	}
	if len(task.Next) > MaxChainLinks {
		return fmt.Errorf("%w (at most %d)", ErrChainTooManyLinks, MaxChainLinks)
	}
	byID := make(map[ID]Task, len(catalog)+1)
	for _, other := range catalog {
		byID[other.ID] = other
	}
	// The proposed definition wins over the persisted one: cycle detection
	// must see the edges being saved, not the edges already on disk.
	byID[task.ID] = task

	for _, link := range task.Next {
		switch link.When {
		case ChainWhenSuccess, ChainWhenFailure, ChainWhenAlways:
		default:
			return fmt.Errorf("%w %q", ErrInvalidChainWhen, link.When)
		}
		if link.DelayMin > 7*24*60 {
			return ErrInvalidChainDelay
		}
		if link.TaskID == task.ID {
			return fmt.Errorf("%w (%s follows itself)", ErrChainCycle, task.ID)
		}
		target, exists := byID[link.TaskID]
		if !exists {
			return fmt.Errorf("%w %q", ErrChainTargetNotFound, link.TaskID)
		}
		if target.ProjectID != task.ProjectID {
			return fmt.Errorf("%w %q", ErrChainCrossProject, link.TaskID)
		}
	}
	if cycle := findChainCycle(byID, task.ID); len(cycle) > 0 {
		return fmt.Errorf("%w (%s)", ErrChainCycle, strings.Join(cycle, " → "))
	}
	if depth := chainLength(byID, task.ID, map[ID]bool{}); depth > MaxChainDepth {
		return fmt.Errorf("%w (%d links, maximum is %d)", ErrChainTooDeep, depth, MaxChainDepth)
	}
	return nil
}

// findChainCycle returns the task ids on the first cycle reachable from root,
// or nil when the reachable sub-graph is acyclic.
func findChainCycle(byID map[ID]Task, root ID) []string {
	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)
	state := make(map[ID]int, len(byID))
	var stack []ID

	var walk func(ID) []string
	walk = func(id ID) []string {
		state[id] = onStack
		stack = append(stack, id)
		for _, link := range byID[id].Next {
			switch state[link.TaskID] {
			case onStack:
				start := 0
				for index, entry := range stack {
					if entry == link.TaskID {
						start = index
						break
					}
				}
				cycle := make([]string, 0, len(stack)-start+1)
				for _, entry := range stack[start:] {
					cycle = append(cycle, string(entry))
				}
				return append(cycle, string(link.TaskID))
			case unvisited:
				if _, exists := byID[link.TaskID]; !exists {
					continue
				}
				if found := walk(link.TaskID); len(found) > 0 {
					return found
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = done
		return nil
	}
	return walk(root)
}

// chainLength is the longest path (in tasks, counting the root) reachable
// from root. visited keeps a cyclic graph from recursing forever; callers that
// care about cycles ask findChainCycle first.
func chainLength(byID map[ID]Task, root ID, visited map[ID]bool) int {
	if visited[root] || len(visited) > MaxChainDepth+1 {
		return 0
	}
	visited[root] = true
	defer delete(visited, root)

	longest := 0
	for _, link := range byID[root].Next {
		if _, exists := byID[link.TaskID]; !exists {
			continue
		}
		if branch := chainLength(byID, link.TaskID, visited); branch > longest {
			longest = branch
		}
	}
	return longest + 1
}

// chainMatches reports whether a settled outcome triggers this edge.
func chainMatches(when ChainWhen, failed bool) bool {
	switch when {
	case ChainWhenAlways:
		return true
	case ChainWhenFailure:
		return failed
	default:
		return !failed
	}
}

// chainContext is the position a settled run occupies. A run that was itself
// triggered carries one; a root run is synthesised as depth 1 so the very
// first notification of a chain already reads "chain 1/3".
func chainContext(task Task, catalog []Task) *ChainRun {
	if task.ActiveChain != nil && task.ActiveChain.Depth > 0 {
		context := *task.ActiveChain
		if total := chainTotal(task, catalog, context.Depth); total > context.Total {
			context.Total = total
		}
		return &context
	}
	if len(task.Next) == 0 {
		return nil
	}
	return &ChainRun{Depth: 1, Total: chainTotal(task, catalog, 1)}
}

func chainTotal(task Task, catalog []Task, depth int) int {
	byID := make(map[ID]Task, len(catalog)+1)
	for _, other := range catalog {
		byID[other.ID] = other
	}
	byID[task.ID] = task
	return depth - 1 + chainLength(byID, task.ID, map[ID]bool{})
}

// chainTargets resolves the edges a settled run should trigger, in a stable
// order so a fan-out is dispatched the same way twice.
func chainTargets(task Task, failed bool) []ChainLink {
	links := make([]ChainLink, 0, len(task.Next))
	for _, link := range task.Next {
		if chainMatches(link.When, failed) {
			links = append(links, link)
		}
	}
	sort.SliceStable(links, func(i, j int) bool {
		return links[i].TaskID < links[j].TaskID
	})
	return links
}
