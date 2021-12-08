package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/knusbaum/mmk"

	"github.com/alecthomas/participle/v2/lexer"
)

const src = `
</sys/lib/whatever.mmk

".*\.o" : ".*\.c"

foo.o : file : foodep
	cc foo.c -o foo.o
	doo bar baz
: fresh : curl
	check-if-fresh $target


blank : foo : rest rest rest
`

func lex(f string) {
	file, err := os.Open(f)
	if err != nil {
		log.Fatalf("Error lexing %s: %s", f, err)
	}
	defer file.Close()
	lx, err := mmk.Lex.Lex(f, file)
	if err != nil {
		log.Printf("Failed to lex: %s", err)
	}
	var t lexer.Token
	for t, err = lx.Next(); err == nil && !t.EOF(); t, err = lx.Next() {
		log.Printf("TOKEN: %#v\n", t)
	}
	log.Printf("Failed to lex: %v %s", t, err)
}

func splitTarget(t string) (string, string) {
	i := strings.LastIndex(t, ":")
	if i == -1 {
		return t, ""
	}
	target := t[:i]
	if target == "" {
		target = "main"
	}
	return target, t[i+1:]
}

func main() {
	mmkfile := flag.String("f", "mmkfile", "the mmkfile to read and execute")
	//ruleType := flag.String("t", "", "the rule type to execute")
	dump := flag.Bool("d", false, "dump the parsed rules to stdout")
	jobs := flag.Int("j", runtime.GOMAXPROCS(-1)+1, "max number of concurrent jobs")
	verbose := flag.Bool("v", false, "run verbosely")
	printTargets := flag.Bool("t", false, "print out all targets available")
	flag.Parse()

	mmk.Verbose = *verbose
	os.Setenv("mmk_verbose", fmt.Sprintf("%t", mmk.Verbose))
	log.SetFlags(log.Ltime)

	if *jobs <= 0 {
		log.Fatalf("Error: jobs must be >= 0")
	}
	os.Setenv("mmk_njobs", fmt.Sprintf("%d", *jobs))

	//lex(*mmkfile)

	res, err := mmk.Parse(*mmkfile)
	if err != nil {
		log.Fatalf("Error: %s", err)
	}
	os.Setenv("mmk_file", *mmkfile)

	if *dump {
		res.Print()
		return
	}

	if *printTargets {
		for _, rs := range res.RuleSets {
			for _, b := range rs.Bodies {
				if b.RuleType != "" {
					fmt.Printf("%s:%s\n", rs.Target, b.RuleType)
				} else {
					fmt.Printf("%s\n", rs.Target)
				}
			}
		}
	}

	targets := flag.Args()
	if len(targets) == 0 {
		targets = []string{"main"}
	}
	for _, target := range targets {
		target, ruleType := splitTarget(target)
		if res.RuleFor(target, ruleType) == nil {
			target += ":" + ruleType
			ruleType = ""
			if res.RuleFor(target, ruleType) == nil {
				log.Fatalf("Could not find target for %s", target)
			}
		}
		//log.Printf("Target: [%s], RuleType: [%s]", target, ruleType)
		if ruleType != "" {
			log.Printf("Starting %s:%s", target, ruleType)
		} else {
			log.Printf("Starting %s", target)
		}
		graph, err := mmk.GenerateGraph(res, target, ruleType)
		if err != nil {
			log.Fatalf("Could not construct dependency graph for %s: %s", target, err)
		}
		err = graph.Execute(*jobs)
		if err != nil {
			log.Fatalf("Failed to build target %s: %s", target, err)
		}
	}
}

func printrec(n *mmk.Node) {
	fmt.Printf("%s(%p)", n.Target, n)
	if len(n.Outgoing) > 0 {
		fmt.Printf(" -> [")
		for name, node := range n.Outgoing {
			fmt.Printf("(%s)@", name)
			printrec(node)
		}
		fmt.Printf("] ")
	}
}
