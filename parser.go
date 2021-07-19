package mmk

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer/stateful"
)

var Lex = stateful.Must(stateful.Rules{
	"Root": {
		{"Colon", `:`, nil},
		{"Var", "var", stateful.Push("Var")},
		{"Include", `^<.*\n`, nil},
		{"String", `"(\\"|[^"])*"`, nil},
		{"Regex", `'(\\'|[^'])*'`, nil},
		{"Any", `\S+`, nil},
		{"CmdLine", `^\t.*`, nil},
		{"Newline", `[\n]`, nil},
		{"whitespace", `[\t\f\r ]+`, nil}, // Rules starting with a lower-case letter are elided automatically.
	},
	"Var": {
		{"Vname", "[a-zA-Z][a-zA-Z0-9_-]*", nil},
		{"newline", `[\n]`, stateful.Pop()},
		{"Equal", "=", stateful.Push("Var2")},
		{"whitespace", `[\t\f\r ]+`, nil},
	},
	"Var2": {
		{"newline", `[\n]`, stateful.Pop()},
		{"VarVal", `.*`, stateful.Pop()},
	},
})

var Parser = participle.MustBuild(&File{}, participle.Lexer(Lex))

type File struct {
	Source     string
	Directives []*Directive `(Newline* @@*)*`
}

type Directive struct {
	Include string `@Include`
	Rule    *Rule  `| @@`
	Var     *Var   `| Var @@`
}

type Var struct {
	Name  string `@Vname`
	Value string `Equal @VarVal`
}

type Rule struct {
	Target       Elem           `@@`
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
	Any    string `@Any`
	String string `| @String`
	Regex  string `| @Regex`
}

func (e *Elem) Value() *Matcher {
	if e.Any != "" {
		return &Matcher{Str: e.Any}
	} else if e.String != "" {
		return &Matcher{Str: strings.Trim(e.String, `"`)}
	} else {
		return &Matcher{Regex: regexp.MustCompile(strings.Trim(e.Regex, `'`))}
	}
}

type Matcher struct {
	Str   string
	Regex *regexp.Regexp
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
	RuleSets []*RuleSet
}

type RuleSet struct {
	Target *Matcher
	Bodies []*RuleBody
}

type RuleBody struct {
	RuleType     string
	Dependencies []string
	Lines        []string
}

func (r *RuleSet) SelectBody(ruleType string) *RuleBody {
	if ruleType == "" {
		return r.Bodies[0]
	}
	for _, body := range r.Bodies {
		// TODO: Allow other ruleType selection methods
		if body.RuleType == ruleType {
			return body
		}
	}
	return nil
}

func (r *RuleSets) RuleFor(target, ruleType string) *RuleSet {
	for _, s := range r.RuleSets {
		if s.Target.Matches(target) && s.SelectBody(ruleType) != nil {
			return s
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
		} else if directive.Var != nil {
			log.Printf("Have Variable: %#v\n", directive.Var)
		} else {
			newDirectives = append(newDirectives, directive)
		}
	}
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
	}
}

func convert(f *File) (*RuleSets, error) {
	err := expand(f)
	if err != nil {
		return nil, err
	}

	var sets []*RuleSet
	for _, d := range f.Directives {
		rs := &RuleSet{Target: d.Rule.Target.Value(), Bodies: make([]*RuleBody, 0)}
		for i, s := range d.Rule.RuleSections {
			var rb RuleBody
			var ruleTypes []string
			if i == 0 && s.Colon == "" {
				// First rule section, if only second section is present, it is dependencies.
				for i := 0; i < len(s.SecondPart); i++ {
					rb.Dependencies = append(rb.Dependencies, s.SecondPart[i].Value().String())
				}
			} else {
				// Otherwise and in the remaining rule sections, second part is
				// *always* the type, third part is optional dependencies.
				for i := 0; i < len(s.SecondPart); i++ {
					ruleTypes = append(ruleTypes, s.SecondPart[i].Value().String())
				}
				for i := 0; i < len(s.ThirdPart); i++ {
					rb.Dependencies = append(rb.Dependencies, s.ThirdPart[i].Value().String())
				}
			}
			rb.RuleType = strings.Join(ruleTypes, " ")
			rb.Lines = s.Lines
			rs.Bodies = append(rs.Bodies, &rb)
			sets = append(sets, rs)
		}
	}
	// sets are searched in reverse read order.
	for i, j := 0, len(sets)-1; i < j; i, j = i+1, j-1 {
		sets[i], sets[j] = sets[j], sets[i]
	}
	return &RuleSets{sets}, nil
}

func Parse(file string) (*RuleSets, error) {
	f, err := parseFile(file)
	if err != nil {
		return nil, err
	}
	return convert(f)
}
