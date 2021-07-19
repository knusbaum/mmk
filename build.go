package mmk

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Exists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

type Node struct {
	Target   string
	RuleType string
	RuleSet  *RuleSet
	Incoming map[string]*Node
	Outgoing map[string]*Node
	visited  bool

	sync.Mutex
	built    chan struct{}
	buildErr error
}

func (n *Node) Wait() error {
	<-n.built
	return n.buildErr
}

func (r *RuleSets) BuildGraph(target, ruleType string, depchain []string, graph map[string]*Node) (*Node, error) {
	rule := r.RuleFor(target, ruleType)
	if rule == nil {
		if fileExists(target) {
			log.Printf("Found dependency <FILE %s>", target)
			return nil, nil
		}
		return nil, fmt.Errorf("No such target %s for dependency chain %s", target, strings.Join(depchain, " -> "))
	}
	node, ok := graph[target]
	if !ok {
		node = &Node{
			Target:   target,
			RuleType: ruleType,
			RuleSet:  rule,
			Incoming: make(map[string]*Node),
			Outgoing: make(map[string]*Node),
			built:    make(chan struct{}),
		}
		graph[target] = node
	}

	body := rule.SelectBody(ruleType)
	for _, dependency := range body.Dependencies {
		dc := append(depchain, target)
		depnode, err := r.BuildGraph(dependency, ruleType, dc, graph)
		if err != nil {
			return nil, err
		}
		if depnode == nil {
			// Non-node dependency (file present)
			continue
		}
		depnode.Incoming[target] = node
		node.Outgoing[dependency] = depnode
	}
	return node, nil
}

func FindRoots(n *Node, roots []*Node) []*Node {
	if n.visited {
		return roots
	}

	n.visited = true
	if len(n.Outgoing) == 0 {
		roots = append(roots, n)
	}
	for _, in := range n.Incoming {
		roots = FindRoots(in, roots)
	}
	for _, out := range n.Outgoing {
		roots = FindRoots(out, roots)
	}
	return roots
}

type Graph struct {
	roots []*Node
}

func GenerateGraph(rs *RuleSets, target, ruleType string) (*Graph, error) {
	graph := make(map[string]*Node)
	start, err := rs.BuildGraph(target, ruleType, []string{}, graph)
	if err != nil {
		return nil, err
	}
	roots := FindRoots(start, nil)
	return &Graph{roots}, nil
}

func (g *Graph) Execute() error {
	var wg sync.WaitGroup
	errc := make(chan error)
	for _, root := range g.roots {
		wg.Add(1)
		go func(root *Node) {
			defer wg.Done()
			if err := Execute(root); err != nil {
				errc <- err
			}
		}(root)
	}
	go func() {
		defer close(errc)
		wg.Wait()
	}()

	// This only collects the first error.
	err, ok := <-errc
	if ok {
		return err
	}
	return nil
}

func addHeader(body string) string {
	return `
set -o errexit
set -o nounset
set -o pipefail

set -x

` + body + `
`
}

func (n *Node) run() error {
	// NOT PROTECTED BY A LOCK (should be run from Build())
	body := n.RuleSet.SelectBody(n.RuleType)
	execBody := strings.Join(body.Lines, "\n")
	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(addHeader(execBody))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("RUN ERROR: %s", err)
		return fmt.Errorf("Failed to execute target: %s: %s", n.Target, err)
	}
	return nil
}

func (n *Node) Build() error {
	n.Lock()
	defer n.Unlock()
	select {
	case <-n.built:
		fmt.Printf("%s already built.\n", n.Target)
		return nil
	default:
	}
	log.Printf("Building %s Incoming: %d, Outgoing: %d\n", n.Target, len(n.Incoming), len(n.Outgoing))
	if err := n.run(); err != nil {
		n.buildErr = err
		log.Printf("ERROR: %s", err)
		return err
	}
	close(n.built)
	return nil
}

func Execute(n *Node) error {
	for _, out := range n.Outgoing {
		err := out.Wait()
		if err != nil {
			return fmt.Errorf("Cannot build %s. Dependency failed: %s", n.Target, err)
		}
	}
	if err := n.Build(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	errc := make(chan error)
	for _, in := range n.Incoming {
		wg.Add(1)
		go func(in *Node) {
			defer wg.Done()
			if err := Execute(in); err != nil {
				errc <- err
			}
		}(in)
	}
	go func() {
		defer close(errc)
		wg.Wait()
	}()

	// This only collects the first error.
	err, ok := <-errc
	if ok {
		return err
	}
	return nil
}
