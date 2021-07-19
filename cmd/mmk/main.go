package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/knusbaum/mmk"
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

func main() {
	mmkfile := flag.String("f", "mmkfile", "the mmkfile to read and execute")
	ruleType := flag.String("t", "", "the rule type to execute")
	flag.Parse()

	res, err := mmk.Parse(*mmkfile)
	if err != nil {
		log.Fatalf("Error: %s", err)
	}

	targets := flag.Args()
	if len(targets) == 0 {
		targets = []string{"main"}
	}
	for _, target := range targets {
		graph, err := mmk.GenerateGraph(res, target, *ruleType)
		if err != nil {
			log.Fatalf("Could not construct dependency graph for %s: %s", target, err)
		}
		err = graph.Execute()
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
