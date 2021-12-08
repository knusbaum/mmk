package mmk

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer/stateful"
)

var Lex = stateful.Must(stateful.Rules{
	"String": {
		{"String", `"(\\"|[^"])*"`, nil},
	},
	"Any": {
		{"Any", `[^'\s\\]+`, nil},
		{"continue", `\\.*\n\s*`, nil},
	},
	"Root": {
		{"comment", `#.*`, nil},
		{"Colon", `:`, nil},
		{"Var", "var", stateful.Push("Var")},
		{"Include", `^<.*\n`, nil},
		{"Ruletype", "ruletype", nil},
		stateful.Include("String"),
		{"Regex", `'(\\'|[^'])*'`, nil},
		stateful.Include("Any"),
		{"CmdLine", `^\t.*`, nil},
		{"Newline", `\n`, nil},
		{"whitespace", `[\t\f\r ]+`, nil}, // Rules starting with a lower-case letter are elided automatically.
	},
	"Var": {
		{"Vname", "[a-zA-Z][a-zA-Z0-9_-]*", nil},
		{"Newline", `\n`, stateful.Pop()},
		{"Equal", "=", stateful.Push("Val")},
		{"whitespace", `[\t\f\r ]+`, nil},
	},
	"Val": {
		stateful.Include("String"),
		stateful.Include("Any"),
		{"whitespace", `[\t\f\r ]+`, nil},
		{"Newline", `\n`, stateful.Push("Root")},
	},
})

var Parser = participle.MustBuild(&File{}, participle.Lexer(Lex))

type File struct {
	Source     string
	Vars       []*Var
	RuleTypes  map[string]*RuleType
	Directives []*Directive `(Newline* @@*)*`
}

type Directive struct {
	Include  string    `@Include`
	Rule     *Rule     `| @@`
	Var      *Var      `| Var @@`
	RuleType *RuleType `| Ruletype @@`
}

type RuleType struct {
	RuleType     *Elem          `@@`
	RuleSections []*RuleSection `(Newline? @@)*`
}

type Var struct {
	Name  string   `@Vname`
	Value []string `Equal @(Any | String | Vname)*`
}

type Rule struct {
	Target       []*Elem        `@@*`
	RuleSections []*RuleSection `@@*`
}

type RuleSection struct {
	SecondPart []Elem   `Colon @@*`
	Colon      string   `@Colon?`
	ThirdPart  []Elem   `@@* Newline`
	Lines      []string `(@CmdLine Newline?)*`
}

type Elems struct {
	Elems []Elem `@@`
}

type Elem struct {
	Any string `@(Any | String)`
	//String string `| @String`
	Regex string `| @Regex`
}

func (e *Elem) Raw() string {
	if e.Any != "" {
		return e.Any
	} else {
		return e.Regex
	}
}

func (e *Elem) Combine(e2 *Elem) *Elem {
	var (
		isRegex bool
		s1, s2  string
	)
	if e.Any == "" {
		s1 = strings.Trim(e.Regex, `'`)
		isRegex = true
	} else {
		s1 = strings.Trim(e.Any, `"`)
	}

	if e2.Any == "" {
		s2 = strings.Trim(e2.Regex, `'`)
		if !isRegex {
			s1 = regexp.QuoteMeta(s1)
		}
		isRegex = true
	} else {
		s2 = strings.Trim(e2.Any, `"`)
	}

	fmt.Printf(" COMBINING [%s] with [%s]\n", s1, s2)
	if isRegex {
		return &Elem{Regex: s1 + s2}
	} else {
		return &Elem{Any: s1 + s2}
	}
}

func (e *Elem) Expand(m map[string]string) {
	if e.Any != "" {
		e.Any = os.Expand(e.Any, func(s string) string {
			return m[s]
		})
	}
}

func (e *Elem) Value() *Matcher {
	if e.Any != "" {
		return &Matcher{Str: strings.Trim(e.Any, `"`)}
	} else {
		return &Matcher{Regex: regexp.MustCompile("^" + strings.Trim(e.Regex, `'`) + "$")}
	}
}

func combineExpandElems(es []*Elem, m map[string]string) *Elem {
	var ret *Elem
	for i, e := range es {
		e.Expand(m)
		if i == 0 {
			ret = e
		} else {
			ret = ret.Combine(e)
		}
	}
	return ret
}

type Matcher struct {
	Str   string
	Regex *regexp.Regexp
}

func (m *Matcher) Captures(s string) []string {
	if m.Str != "" {
		return nil
	}
	return m.Regex.FindStringSubmatch(s)
}

func (m *Matcher) Matches(s string) bool {
	//log.Printf("Checking (%s)%#v matches %s", m, m, s)
	//defer func() { log.Printf("Returning %t", ret) }()
	if m.Str != "" {
		return m.Str == s
	}
	return m.Regex.MatchString(s)
}

func (m *Matcher) String() string {
	if m.Str != "" {
		return m.Str
	}
	return m.Regex.String()
}

type RuleSets struct {
	Vars     []*Var
	RuleSets []*RuleSet
}

type RuleSet struct {
	Target *Matcher
	Bodies []*RuleBody
}

type RuleBody struct {
	RuleType     string
	FailOK       bool
	Dependencies []string
	Lines        []string
}

func (r *RuleSet) SelectBody(ruleType string) *RuleBody {
	if ruleType == "" {
		if len(r.Bodies) > 0 {
			return r.Bodies[0]
		}
		return nil
	}
	for _, body := range r.Bodies {
		// TODO: Allow other ruleType selection methods
		if body.RuleType == ruleType {
			return body
		}
	}
	return nil
}

func (r *RuleSets) Print() {
	fmt.Printf("[Vars: \n")
	for _, v := range r.Vars {
		fmt.Printf("\t%s=%#v\n", v.Name, v.Value)
	}
	fmt.Printf("]\n")
	for _, rs := range r.RuleSets {
		fmt.Printf("[Target: %s]\n", rs.Target)
		for _, body := range rs.Bodies {
			fmt.Printf("\t [Type: %s] -> [Deps: %s]:\n", body.RuleType, strings.Join(body.Dependencies, ", "))
			for _, line := range body.Lines {
				fmt.Printf("\t\t%s\n", line)
			}
		}
	}
}

func (r *RuleSets) RuleFor(target, ruleType string) *RuleSet {
	for _, s := range r.RuleSets {
		//log.Printf("Finding %s:%s Checking for target %s", target, ruleType, s.Target.String())
		//log.Printf("TARGET MATCHES: %t, BODY(%s): %p", s.Target.Matches(target), ruleType, s.SelectBody(ruleType))
		if s.Target.Matches(target) && s.SelectBody(ruleType) != nil {
			//log.Printf("RETURNING %p", s)
			return s
		} else {
			//log.Printf("RETURNING NIL!")
		}
	}
	return nil
}

func parseFile(file string) (*File, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ret := &File{}
	err = Parser.Parse(file, bufio.NewReader(f), ret)
	if err != nil {
		return nil, err
	}
	ret.Source = file
	sanitize(ret)
	return ret, nil
}

func expand(f *File) error {
	var newDirectives []*Directive
	ruleTypes := make(map[string]*RuleType)
	for _, directive := range f.Directives {
		if directive.Include != "" {
			incf, err := parseFile(directive.Include)
			if err != nil {
				return fmt.Errorf("In File %s: <%s: %s", f.Source, directive.Include, err)
			}
			err = expand(incf)
			if err != nil {
				return err
			}
			newDirectives = append(newDirectives, incf.Directives...)
			for k, rt := range incf.RuleTypes {
				ruleTypes[k] = rt
			}
			f.Vars = append(f.Vars, incf.Vars...)
		} else if directive.Var != nil {
			joined := strings.Join(directive.Var.Value, " ")
			//joined := directive.Var.Value
			if strings.HasPrefix(joined, "$(") && strings.HasSuffix(joined, ")") {
				cmdBody := joined[2:]
				cmdBody = cmdBody[:len(cmdBody)-1]
				//log.Printf("CMD: %s\n", cmdBody)
				cmd := exec.Command("bash", "-c", cmdBody)
				//cmd.Stdin = strings.NewReader(addHeader(execBody, n.Target, n.Vars))
				cmd.Stderr = os.Stderr
				output, err := cmd.Output()
				if err != nil {
					log.Printf("%s Error: %s", cmdBody, err)
					directive.Var.Value = []string{""}
					//directive.Var.Value = ""
				} else {
					directive.Var.Value = []string{strings.TrimSpace(string(output))}
					//directive.Var.Value = strings.TrimSpace(string(output))
				}
			}
			//log.Printf("Have Variable: %#v\n", directive.Var)
			f.Vars = append(f.Vars, directive.Var)
		} else if directive.RuleType != nil {
			ruleTypes[directive.RuleType.RuleType.Value().String()] = directive.RuleType
			//log.Printf("Have RuleType:")
			//spew.Dump(directive.RuleType)
		} else if directive.Rule != nil {
			newDirectives = append(newDirectives, directive)
		}
	}
	f.RuleTypes = ruleTypes
	f.Directives = newDirectives
	return nil
}

func sanitize(f *File) {
	for _, directive := range f.Directives {
		if directive.Include != "" {
			directive.Include = strings.TrimSpace(directive.Include[1:])
		}
		if directive.Rule != nil {
			for _, section := range directive.Rule.RuleSections {
				for i := range section.Lines {
					section.Lines[i] = strings.TrimSpace(section.Lines[i])
				}
			}
		}
		if directive.RuleType != nil {
			for _, section := range directive.RuleType.RuleSections {
				for i := range section.Lines {
					section.Lines[i] = strings.TrimSpace(section.Lines[i])
				}
			}
		}
	}
}

func convert(f *File) (*RuleSets, error) {
	err := expand(f)
	if err != nil {
		return nil, err
	}

	var sets []*RuleSet
	for _, d := range f.Directives {
		defaults := make(map[string]*RuleSet)
		for ruleType, rt := range f.RuleTypes {
			rs := &RuleSet{Bodies: make([]*RuleBody, 0)}
			for _, s := range rt.RuleSections {
				var rb RuleBody
				var ruleTypes []string
				// for ruletype definitions, second part is
				// *always* the type, third part is optional dependencies.
				for i := 0; i < len(s.SecondPart); i++ {
					ruleTypes = append(ruleTypes, s.SecondPart[i].Value().String())
				}
				if s.Colon != "" {
					// We are using a non-nil Dependencies to differentiate between unspecified and
					// specified but empty.
					rb.Dependencies = make([]string, 0)
				}
				for i := 0; i < len(s.ThirdPart); i++ {
					rb.Dependencies = append(rb.Dependencies, s.ThirdPart[i].Raw())
				}
				for i, t := range ruleTypes {
					if i == 0 {
						rb.RuleType = t
					}
					if t == "failok" {
						rb.FailOK = true
					}
				}
				rb.Lines = s.Lines
				rs.Bodies = append(rs.Bodies, &rb)
			}
			defaults[ruleType] = rs
		}

		vars := make(map[string]string)
		for _, v := range f.Vars {
			vars[v.Name] = strings.Join(v.Value, " ")
		}

		types := make(map[string]struct{})
		rs := &RuleSet{Target: combineExpandElems(d.Rule.Target, vars).Value(), Bodies: make([]*RuleBody, 0)}
		for i, s := range d.Rule.RuleSections {
			var rb RuleBody
			var ruleTypes []string
			if i == 0 && s.Colon == "" {
				// First rule section, if only second section is present, it is dependencies.
				for i := 0; i < len(s.SecondPart); i++ {
					rb.Dependencies = append(rb.Dependencies, s.SecondPart[i].Raw())
				}
			} else {
				// Otherwise and in the remaining rule sections, second part is
				// *always* the type, third part is optional dependencies.
				for i := 0; i < len(s.SecondPart); i++ {
					ruleTypes = append(ruleTypes, s.SecondPart[i].Value().String())
				}
				if s.Colon == "" {
					// for subsequent rules, if no dep list is specified, inherit from the first rule.
					rb.Dependencies = rs.Bodies[0].Dependencies
				} else {
					for i := 0; i < len(s.ThirdPart); i++ {
						rb.Dependencies = append(rb.Dependencies, s.ThirdPart[i].Raw())
					}
				}
			}
			for i, t := range ruleTypes {
				if i == 0 {
					rb.RuleType = t
				}
				if t == "failok" {
					rb.FailOK = true
				}
			}

			//ruleTypes[0] //strings.Join(ruleTypes, " ")
			if _, ok := types[rb.RuleType]; ok {
				return nil, fmt.Errorf("Duplicate definition for target %s", combineExpandElems(d.Rule.Target, vars).Value())
			}
			types[rb.RuleType] = struct{}{}
			rb.Lines = s.Lines
			rs.Bodies = append(rs.Bodies, &rb)
		}
		// apply any defaults
		var additional []*RuleBody
		for i, body := range rs.Bodies {
			if defaults, ok := defaults[body.RuleType]; ok {
				for _, b := range defaults.Bodies {
					if rs.Bodies[0].Dependencies != nil {
						b.Dependencies = rs.Bodies[0].Dependencies
					}
					// 					if b.Dependencies == nil {
					// 						b.Dependencies = rs.Bodies[0].Dependencies
					// 					}

					if len(body.Lines) == 0 && b.RuleType == body.RuleType {
						rs.Bodies[i] = b
					} else {
						additional = append(additional, b)
					}
				}
			}
		}
		for _, body := range additional {
			if _, ok := types[body.RuleType]; ok {
				continue
			}
			types[body.RuleType] = struct{}{}
			rs.Bodies = append(rs.Bodies, body)
		}
		sets = append(sets, rs)
	}
	// sets are searched in reverse read order.
	for i, j := 0, len(sets)-1; i < j; i, j = i+1, j-1 {
		sets[i], sets[j] = sets[j], sets[i]
	}
	return &RuleSets{f.Vars, sets}, nil
}

func Parse(file string) (*RuleSets, error) {
	f, err := parseFile(file)
	if err != nil {
		return nil, err
	}
	return convert(f)
}
