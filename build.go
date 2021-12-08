package mmk

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer/stateful"
)

var Verbose bool

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
	Vars     []*Var
	visited  bool
	queued   bool

	sync.Mutex
	built    chan struct{}
	buildErr error
}

func (n *Node) Wait() error {
	<-n.built
	return n.buildErr
}

var depLex = stateful.MustSimple([]stateful.Rule{
	{"String", `"(\\"|[^"])*"`, nil},
	{"Part", `[^\s:]+`, nil},
	{"Colon", `:`, nil},
	{"Whitespace", `\s`, nil},
})

type dep struct {
	Target   string `@(String | Part)`
	Colon    string `@Colon?`
	RuleType string `@(String | Part)?Whitespace*`
}

type deps struct {
	Deps []*dep `@@*`
}

var depParser = participle.MustBuild(&deps{}, participle.Lexer(depLex))

func fileRuleSet(target string) *RuleSet {
	return &RuleSet{Target: &Matcher{Str: target}, Bodies: []*RuleBody{{Dependencies: make([]string, 0), Lines: make([]string, 0)}}}
}

func (r *RuleSets) BuildGraph(target, ruleType string, depchain []string, graph map[string]*Node) (*Node, error) {
	// log.Printf("depchain: %#v\n", depchain)
	for _, dep := range depchain {
		if dep == target {
			return nil, fmt.Errorf("Found dependency cycle: %s", strings.Join(depchain, " -> ")+" -> "+target)
		}
	}
	rule := r.RuleFor(target, ruleType)
	if rule == nil {
		if ruleType == "" && fileExists(target) {
			// 			if Verbose {
			// 				log.Printf("No rule found for %s, but found file with same name.", target)
			// 			}
			rule = fileRuleSet(target)
		} else {
			targetrule := target
			if ruleType != "" {
				targetrule += ":" + ruleType
			}
			return nil, fmt.Errorf("No such target %s for dependency chain %s", targetrule, strings.Join(append(depchain, targetrule), " -> "))
		}
	}
	node, ok := graph[target+":"+ruleType]
	if !ok {
		node = &Node{
			Target:   target,
			RuleType: ruleType,
			RuleSet:  rule,
			Incoming: make(map[string]*Node),
			Outgoing: make(map[string]*Node),
			Vars:     r.Vars,
			built:    make(chan struct{}),
		}
		graph[target+":"+ruleType] = node
	}

	body := rule.SelectBody(ruleType)

	vars := make(map[string]string)
	for _, v := range r.Vars {
		vars[v.Name] = strings.Join(v.Value, " ")
	}
	strs := node.RuleSet.Target.Captures(node.Target)
	for i, s := range strs {
		vars[fmt.Sprintf("match_%d", i)] = s
	}
	vars["target"] = node.Target
	dependencystr := strings.Join(body.Dependencies, " ")
	dependencystr = os.Expand(dependencystr, func(s string) string {
		return vars[s]
	})

	var ds deps
	err := depParser.ParseString("", dependencystr, &ds)
	if err != nil {
		log.Printf("Failed to parse dependencies: %s", err)
	}

	dc := append(depchain, target+":"+ruleType)

	for _, dep := range ds.Deps {
		depTarget := strings.Trim(dep.Target, `"`)
		rt := ruleType
		if dep.Colon != "" {
			rt = dep.RuleType
		}
		depnode, err := r.BuildGraph(depTarget, rt, dc, graph)
		if err != nil {
			if body.FailOK {
				if Verbose {
					log.Printf("Cannot build dependency %s:%s: %s", depTarget, rt, err)
					log.Printf("%s:%s is failok. Skipping %s:%s", target, ruleType, depTarget, rt)
				}
				continue
			}
			return nil, err
		}
		if depnode == nil {
			// Non-node dependency (file present)
			continue
		}
		depnode.Incoming[target+":"+ruleType] = node
		node.Outgoing[depTarget+":"+rt] = depnode
	}
	//log.Printf("%s:%s RETURNING NODE: %v", target, ruleType, node)
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

func (g *Graph) Execute(njobs int) error {
	return Execute(g.roots, njobs)
	// 	var wg sync.WaitGroup
	// 	errc := make(chan error)
	// 	for _, root := range g.roots {
	// 		wg.Add(1)
	// 		go func(root *Node) {
	// 			defer wg.Done()
	// 			if err := Execute(root, njobs); err != nil {
	// 				errc <- err
	// 			}
	// 		}(root)
	// 	}
	// 	go func() {
	// 		defer close(errc)
	// 		wg.Wait()
	// 	}()
	//
	// 	// This only collects the first error.
	// 	err, ok := <-errc
	// 	if ok {
	// 		return err
	// 	}
	// 	return nil
}

func addHeader(body string) string {
	ret := `
set -o errexit
set -o nounset
set -o pipefail
`
	if Verbose {
		ret += `set -x
`
	}
	ret += `

function mmkecho {
	builtin echo $@ 1>&3
}

` + body + `
`
	//fmt.Printf("SCRIPT: %s", ret)
	return ret
}

func (n *Node) run() error {
	// NOT PROTECTED BY A LOCK (should be run from Build())
	body := n.RuleSet.SelectBody(n.RuleType)
	execBody := strings.Join(body.Lines, "\n")
	cmd := exec.Command("bash", "-s")
	var vars []string
	for _, v := range n.Vars {
		vars = append(vars, fmt.Sprintf("%s=%s", v.Name, strings.Join(v.Value, " ")))
	}
	strs := n.RuleSet.Target.Captures(n.Target)
	for i, s := range strs {
		vars = append(vars, fmt.Sprintf("match_%d=%s", i, s))
	}
	vars = append(vars, fmt.Sprintf("mmk_ruletype=%s", n.RuleType))
	vars = append(vars, fmt.Sprintf("target=%s", n.Target))
	cmd.Env = append(os.Environ(), vars...)
	cmd.Stdin = strings.NewReader(addHeader(execBody))
	if Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	cmd.ExtraFiles = []*os.File{os.Stderr}
	if err := cmd.Run(); err != nil && !body.FailOK {
		log.Printf("RUN ERROR: %s", err)
		return fmt.Errorf("Failed to execute target: %s: %s", n.Target, err)
	}
	return nil
}

func (n *Node) BuildDate() time.Time {
	for _, body := range n.RuleSet.Bodies {
		if body.RuleType == "build_date" {
			execBody := strings.Join(body.Lines, "\n")
			cmd := exec.Command("bash", "-s")
			var vars []string
			for _, v := range n.Vars {
				vars = append(vars, fmt.Sprintf("%s=%s", v.Name, v.Value))
			}
			strs := n.RuleSet.Target.Captures(n.Target)
			for i, s := range strs {
				vars = append(vars, fmt.Sprintf("match_%d=%s", i, s))
			}
			vars = append(vars, fmt.Sprintf("target=%s", n.Target))
			cmd.Env = append(os.Environ(), vars...)
			cmd.Stdin = strings.NewReader(addHeader(execBody))
			if Verbose {
				cmd.Stderr = os.Stderr
			}
			cmd.ExtraFiles = []*os.File{os.Stderr}
			output, err := cmd.Output()
			if err != nil {
				//log.Printf("Failed to run build_date target for target %s: %s", n.Target, err)
				return time.Time{}
			}
			t, err := time.Parse(time.RFC1123Z, strings.TrimSpace(string(output)))
			if err != nil {
				log.Printf("Failed to parse date from build_date target for target %s: %s [Output: %s]", n.Target, err, strings.TrimSpace(string(output)))
				return time.Time{}
			}
			return t
		}
	}
	stat, err := os.Stat(n.Target)
	if err != nil {
		return time.Time{}
	}
	return stat.ModTime()
}

func (n *Node) NeedsBuild() bool {
	//log.Printf("CHECKING TARGET [%s]", n.Target)
	if n.RuleType == "" {
		//log.Printf("Checking Build Date.")
		thisDate := n.BuildDate()
		if thisDate.IsZero() {
			return true
		}
		for _, out := range n.Outgoing {
			upstream := out.BuildDate()
			if upstream.After(thisDate) {
				return true
			}
		}
		return false
	}
	return true
}

func (n *Node) Fail(err error) {
	n.Lock()
	defer n.Unlock()
	n.buildErr = err
	log.Printf("ERROR: %s", err)
	close(n.built)
}

func (n *Node) Build() error {
	n.Lock()
	defer n.Unlock()
	select {
	case <-n.built:
		//log.Printf("!!!%s already built.\n", n.Target)
		return nil
	default:
	}
	if !n.NeedsBuild() {
		if Verbose {
			if n.RuleType != "" {
				log.Printf("%s:%s already built.", n.Target, n.RuleType)
			} else {
				log.Printf("%s already built.", n.Target)
			}
		}
		close(n.built)
		return nil
	}
	body := n.RuleSet.SelectBody(n.RuleType)
	if body.RuleType != "" {
		log.Printf("Building %s:%s", n.Target, body.RuleType)
	} else {
		log.Printf("Building %s", n.Target)
	}
	if err := n.run(); err != nil {
		n.buildErr = err
		log.Printf("ERROR: %s", err)
		close(n.built)
		return err
	}
	//log.Printf("CLOSING BUILT FOR %s", n.Target)
	close(n.built)
	return nil
}

// func execute(n *Node) error {
// 	for _, out := range n.Outgoing {
// 		err := out.Wait()
// 		if err != nil {
// 			return fmt.Errorf("Cannot build %s. Dependency failed: %s", n.Target, err)
// 		}
// 	}
// 	if err := n.Build(); err != nil {
// 		return err
// 	}
// 	var wg sync.WaitGroup
// 	errc := make(chan error)
// 	for _, in := range n.Incoming {
// 		wg.Add(1)
// 		go func(in *Node) {
// 			defer wg.Done()
// 			if err := execute(in); err != nil {
// 				errc <- err
// 			}
// 		}(in)
// 	}
// 	go func() {
// 		defer close(errc)
// 		wg.Wait()
// 	}()
//
// 	// This only collects the first error.
// 	err, ok := <-errc
// 	if ok {
// 		return err
// 	}
// 	return nil
// }

func enqueue(execNode chan *Node, ns []*Node) {
	queue := make([]*Node, len(ns))
	copy(queue, ns)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		n.queued = true
		execNode <- n
	queuer:
		for _, n := range n.Incoming {
			if n.queued {
				continue
			}
			for _, out := range n.Outgoing {
				if !out.queued {
					continue queuer
				}
			}
			queue = append(queue, n)
		}
	}
}

func Execute(ns []*Node, njobs int) error {
	execNode := make(chan *Node, 10)
	errc := make(chan error, 10)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			//defer log.Printf("Exiting %d", i)
			for n := range execNode {
				//log.Printf("(%d) Build %s", i, n.Target)
				for _, out := range n.Outgoing {
					//log.Printf("Waiting for %s", out.Target)
					err := out.Wait()
					//log.Printf("Done Waiting for %s", out.Target)
					if err != nil {
						errc <- fmt.Errorf("Cannot build %s. Dependency failed: %s", n.Target, err)
						n.Fail(err)
						return
					}
				}
				//log.Printf("Actor building %s", n.Target)
				if err := n.Build(); err != nil {
					errc <- err
					return
				}
			}
		}()
	}
	go func() {
		defer close(errc)
		wg.Wait()
	}()

	enqueue(execNode, ns)
	close(execNode)
	var err error
	for e := range errc {
		err = e
		log.Printf("ERROR: %s", err)
	}
	return err
}
