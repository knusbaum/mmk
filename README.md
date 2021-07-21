# mmk

Mmk is a modernized version of the make build tool (Modern MaKe)

It incorporates several improvements to a traditional make that improve its
usability as a modern build tool

## Installation

```
go get github.com/knusbaum/mmk/cmd/mmk
```

## Flags
```
  -d	dump the parsed rules to stdout
  -f string
    	the mmkfile to read and execute (default "mmkfile")
  -j int
    	max number of concurrent jobs (default GOMAXPROCS+1)
  -v	run verbosely
```

## Features

### Rule-based Target Definitions

Mmk works like make and other make variants by describing "rules" to build
"targets", which are usually artifact. The most basic rule definition
consists of a header describing the target, followed by a series of
tab-prefixed lines of bash script.

In the script, `$target` is set to the name of the target.

For example, you could describe a rule to build the file `timefile`:
```
timefile :
	date > timefile
```

Mmk will execute the rule passed to it:
```
$ mmk timefile
01:02:03 Starting timefile
01:02:03 Building timefile
```

Mmk supresses output from rules, but rules can voluntarily echo things to
mmk's standard error with the `mmkecho` command:
```
timefile :
	mmkecho building timefile
	date >timefile
```

Alternately, mmk can be invoked with the `-v` flag, which will cause all
commands' standard output and standard errors to be connected to mmk`s
standard output and standard error when they are run. No attempt is made to
synchronize output, so when running multiple targets concurrently, expect
output to be jumbled.

#### Dependencies

Rule headers can contain dependencies, which are either file names, or the
names of other rules described in the mmkfile.

```
timefile : 
	date > timefile

# Target "other" depends on target "timefile"
other : timefile
	cat timefile > other
	echo "other now contains the contents of timefile"
```

By default, mmk looks for a file named after the target and uses its
modification time to determine if a target needs to be rebuilt. For
instance, in the above mmkfile, if the files `timefile` and `other` both
exist, but `timefile` is newer than `other`, `other` will be rebuilt.
Likewise, if `other` exists, but `timefile` does not, mmk will rebuild
`timefile`, notice it's newer than `other` and then rebuild `other`.

By default, the same rule type is executed for dependencies as is being
executed for a given rule. However, alternate rule types can be executed by
specifying with the `target:ruletype` syntax. Targets containing colons
(`:`) in their names can be specified as dependencies by quoting them. The
dependency `foo:bar` is target `foo` with rule type `bar` whereas the
dependency `"foo:bar"` is the target `foo:bar` with the default rule type.
See [Typed Rules](#typed-rules) for details on rule types.

#### Regular-Expression Matching for Targets

Mmk targets can be specified with regular expressions. The variable
`$target` is always set to the target name, and subgroup matches are
captured and assigned to variables named `match_[0-9]+`

Regular expression targets need to be defined inside single-quotes. They
contain an implicit `^` at the start and `$` at the end, meaning they must
match the complete target, not just a portion of it - the rule
`myrule\.([0-9]+)` will match `myrule.123` but not `rule.123` or
`myrule.123abc`

For example, the following rule will build any target matching the regular
expression `rule\.([0-9]+)' and the integer after the dot will be captured
into the `$match_1` variable.
```mmkfile
'myrule\.([0-9]+)' : 
	echo "executing rule $target (number $match_1)"
```
```
$ mmk myrule.1234
01:02:03 Starting myrule.1234
01:02:03 Building myrule.1234
executing rule myrule.1234 (number 1234)
```

Rule searching is done in reverse definition order, meaning targets defined
later are checked first. This allows one to specify overlapping
definitions, or special cases for specific targets:
```
`myrule\.([0-9]+)` :
	echo "executing generic myrule target for myrule number $match_1"

myrule.123 :
	echo "Executing special rule for 123"
```
```
$ mmk -v myrule.1234
01:02:03 Starting myrule.1234
01:02:03 Building myrule.1234
executing generic myrule target for myrule number 1234
$ mmk -v myrule.123
01:02:03 Starting myrule.123
01:02:03 Building myrule.123
Executing special rule for 123
```

#### Typed Rules

Rules can have a type. Types for a rule are defined by adding an additional
section (separated by a colon) after the target name. Note the need for the
trailing colon, so that the type is not interpreted as a dependency:
```
# target foo with rule of type mytype
foo : mytype :
	touch foo

# target foo with dependency mytype
foo : mytype
	touch foo

# target foo with dependencies bar and baz
foo : bar baz
	touch foo

# target foo with rule mytype and dependencies bar and baz 
foo : mytype : bar baz
	touch foo
```

You may define more than one rule for a target. This is done by adding an
additional rule header without the target name, with a rule type followed
by another series of tab-prefixed lines. With traditional `make`, `clean`
is usually defined as a target. With `mmk`, `clean` can be defined as a
separate rule for a target:
```
foo : mytype :
	set -x
	echo "foo" > foo
: clean
	set -x
	rm foo
```

Specific rule types for a target are executed by naming `target:ruletype`:
```
$ mmk -v foo:mytype
01:02:03 Starting foo:mytype
01:02:03 Building foo:mytype
+ echo foo
$ mmk -v foo:clean
01:02:03 Starting foo:clean
01:02:03 Building foo:clean
+ rm foo
```

When executing a target without specifying the rule type, the first rule is chosen:
```
$ mmk -v foo
01:02:03 Starting foo
01:02:03 Building foo:mytype
+ echo foo
```

Subsequent rules that have no dependencies defined inherit the dependencies
of the first rule, but they may also define their own dependencies.
```
foo : mytype : dep1 dep2
	touch foo
: clean # clean does not define dependencies, and so inherits dep1 and dep2 from the mytype rule.
	rm foo

foo : mytype : dep1 dep2
	touch foo
: clean : # clean defines empty dependencies, so does not inherit dep1 or dep2. It has no dependencies.
	rm foo

foo2 : mytype : dep1 dep2
	touch foo2
: clean : dep3 dep4 # clean defines its own dependencies, dep3 and dep4
	rm foo2
```

Dependency inheritance is useful for things like clean, since a clean rule
that inherits the dependencies will execute the clean rules of all the
target's dependencies as well.

For example:
```
foo : bar
	echo making $target
	touch $target
: clean
	echo deleting $target
	rm $target

bar : baz
	echo making $target
	touch $target
: clean
	echo deleting $target
	rm $target

baz :
	echo making $target
	touch $target
: clean
	echo deleting $target
	rm $target
```
```
$ mmk -v foo
01:02:03 Starting foo
01:02:03 Building baz
making baz
01:02:03 Building bar
making bar
01:02:03 Building foo
making foo
$ mmk -v foo:clean
01:02:03 Starting foo:clean
01:02:03 Building baz:clean
deleting baz
01:02:03 Building bar:clean
deleting bar
01:02:03 Building foo:clean
deleting foo
```

Rule types can be more than one word. Subsequent words in an rule type are
considered "flags" that attach behavior to the rule. Currently the only
"flag" available is `failok`, which does not cause mmk to stop processing a
target when the `failok` rule fails. This is useful, for example, for
`clean` targets when we don't care if they fail or not.
```
foo :
	echo making $target
	touch $target
: clean failok
	echo deleting $target
	rm $target
```

These rule types can be used in combination with regular expression
matching to achieve complicated behavior. For example, we can define build
rules for targets and share a clean rule:
```
'foo\.([0-9]+)' :
: clean
	echo "Cleaning $target"
	rm $target

foo.123 :
	echo "Putting a date into $target"
	date >$target

foo.456 :
	echo "Putting a random number into $target"
	echo $RANDOM >$target
```
```
$ mmk -v foo.123
01:02:03 Starting foo.123
01:02:03 Building foo.123
Putting a date into foo.123
$ mmk -v foo.456
01:02:03 Starting foo.456
01:02:03 Building foo.456
Putting a random number into foo.456
$ mmk -v foo.123:clean
01:02:03 Starting foo.123:clean
01:02:03 Building foo.123:clean
Cleaning foo.123
$ mmk -v foo.456:clean
01:02:03 Starting foo.456:clean
01:02:03 Building foo.456:clean
Cleaning foo.456
```

#### Build Date Rule

As mentioned before, by default mmk looks for a file named after the target
and uses its modification time and the times of its upstream dependencies
to determine if a target needs to be rebuilt.

Mmk is designed to be used to build artifacts other than files, so it
defines a special rule type, `build_date`, which, when provided, mmk will
use to determine the date and time at which an artifact was built.

The `build_date` rule body should contain commands which output a date on
standard output. This date should be in RFC 5322 / RFC 2822 / RFC 1123
format - this is the format given by `date -j -R` and the date described by
go's [time.RFC1123Z](https://pkg.go.dev/time#pkg-constants).

If the `build_date` rule has a non-zero exit, or the output is not
parseable as a date in the specified format, mmk will consider the target
out of date and build it.

For example, mmk can be used to create docker images. In order to specify
the `build_date` of a docker images, the following rule can be used, where
$target is the name of a docker image:
```
...
: build_date
	date -j -f '%Y-%m-%dT%T' -R $(docker inspect -f '{{ .Created }}' $target) 2>/dev/null
```

### Rule Type Definitions

Rule types let you specify multiple named rules for a given target, but you
can also create rule type definitions that allow you to attach default
behavior to rules of a given type.

Rule type definitions are created by writing `ruletype [name]` followed by
lines containing rule definitions similar to target definitions.

For example, we can create the ruletype `foo`. We attach two bodies, one
default body for a target of type `foo` which creates the target, and one
body `clean`, which removes the file:
```
ruletype foo
: foo
	echo "Creating $target, which is a foo."
	echo foo >$target
: clean
	echo "Deleting $target"
	rm $target
```

We can then create targets of this type, which will exhibit the default
behavior:
```
myfile : foo :
myotherfile : foo :
```

```
$ mmk -v myfile
01:02:03 Starting myfile
01:02:03 Building myfile:foo
Creating myfile, which is a foo.
$ cat myfile
foo
$ mmk -v myotherfile
01:02:03 Starting myotherfile
01:02:03 Building myotherfile:foo
Creating myotherfile, which is a foo.
$ cat myotherfile
foo
$ mmk -v myfile:clean
01:02:03 Starting myfile:clean
01:02:03 Building myfile:clean
Deleting myfile
$ mmk -v myotherfile:clean
01:02:03 Starting myotherfile:clean
01:02:03 Building myotherfile:clean
Deleting myotherfile
```

Non-default behavior can also be specified:
```
# myfile overrides the default build rule
myfile : foo :
	echo Overrode the default build rule for $target
	touch myfile

# myotherfile overrides the clean build rule
myotherfile : foo :
: clean
	echo Overrode the clean build rule for $target
	rm $target
```
```
$ mmk -v myfile
01:02:03 Starting myfile
01:02:03 Building myfile:foo
Overrode the default build rule for myfile
$ mmk -v myotherfile
01:02:03 Starting myotherfile
01:02:03 Building myotherfile:foo
Creating myotherfile, which is a foo.
$ mmk -v myfile:clean
01:02:03 Starting myfile:clean
01:02:03 Building myfile:clean
Deleting myfile
$ mmk -v myotherfile:clean
01:02:03 Starting myotherfile:clean
01:02:03 Building myotherfile:clean
Overrode the clean build rule for myotherfile
```

### Variables

Mmk supports variable declarations of the following forms:
```
var name = value
var name = $(shell command)
```
In the first case, `$name` will be assigned `value`, and in the second
case, `$name` will be assigned to the output of `shell command`. `shell
command` is executed in bash, so supports full bash syntax.

These variables can be used in rule bodies as well as in dependency lists.
When used in dependency lists, the value is separated by spaces into
individual dependencies.

```
ruletype echo
: echo
	echo "Building $target"

var deps = foo bar baz boo

target : echo : $deps
foo : echo :
bar : echo :
baz : echo :
boo : echo :
```
```
$ mmk -v target
01:02:03 Starting target
01:02:03 Building foo:echo
01:02:03 Building baz:echo
01:02:03 Building boo:echo
01:02:03 Building bar:echo
Building foo
Building baz
Building boo
Building bar
01:02:03 Building target:echo
Building target
```

This works with all variables, including ones output from a shell script:
```
ruletype echo
: echo
	echo "Building $target"

# Deps are all files starting with dep
var deps = $(cat mydeps)

target : echo : $deps
foo : echo :
bar : echo :
baz : echo :
boo : echo :
```
```
$ echo foo >mydeps
$ mmk -v target
01:02:03 Starting target
01:02:03 Building foo:echo
Building foo
01:02:03 Building target:echo
Building target
$ echo foo bar baz boo >mydeps
$ mmk -v target
01:02:03 Starting target
01:02:03 Building bar:echo
01:02:03 Building baz:echo
01:02:03 Building foo:echo
01:02:03 Building boo:echo
Building bar
Building baz
Building foo
Building boo
01:02:03 Building target:echo
Building target
```

### Special Syntax

* Mmk supports inline comments. Everything on a line after `#` is ignored
* Mmk also supports line splitting. A line can be continued onto the next line with a `\` character.
* Mmk supports quoted (`"`) strings in most places to capture targets/deps/etc containing spaces or other syntax characters.

For example:
```
# Hello, this is a comment.

"my weird rule name" : dep1 \
                       dep2 \
                       dep3
	echo "This is the rule body"

'dep[0-9]' : # this target does nothing
```
```
$ mmk -v 'my weird rule name'
01:02:03 Starting my weird rule name
01:02:03 Building dep1
01:02:03 Building dep2
01:02:03 Building dep3
01:02:03 Building my weird rule name
This is the rule body
```

### EBNF

Here is the token set and EBNF for an mmkfile:
```
<newline>=\n
<include>=<.*\n
<var>=var
<ruletype>=ruletype
<any>=\S+
<string>="(\\"|[^"])*"
<regex>='(\\'|[^'])*'
<colon>=:
<cmdline>=^\t.*
<vname>=[a-zA-Z][a-zA-Z0-9_-]*

File = (<newline>* Directive*)* .
Directive = <include> | Rule | (<var> Var) | (<ruletype> RuleType) .
Rule = Elem RuleSection* .
Elem = (<any> | <string>) | <regex> .
RuleSection = <colon> Elem* <colon>? Elem* <newline> (<cmdline> <newline>?)* .
Var = <vname> "=" (<any> | <string> | <vname>)* .
RuleType = Elem (<newline>? RuleSection)* .
```
