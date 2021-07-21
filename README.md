# mmk

MMK is a modernized version of the make build tool (Modern MaKe)

It incorporates several improvements to a traditional make that improve its
usability as a modern build tool

## Installation

```
go get github.com/knusbaum/mmk/cmd/mmk
```

## Features

### Rule-based Target Definitions

Mmk works like make and other make variants by describing "rules" to build
"targets", which are usually artifacts that can be built. The most basic
rule definition consists of a header describing the target, followed by a
series of tab-prefixed lines of bash script.

In the script, `$target` is set to the name of the target.

For example, you could describe a rule to build the file `timefile`:
```
timefile :
	date > timefile
```

#### Dependencies

Rule headers can contain dependencies, which are either file names, or the names of other rules described in the mmkfile.

```
timefile : 
	date > timefile

other : timefile
	cat timefile > other
	echo "other now contains the contents of timefile"
```

By default, mmk looks for a file named after the target and uses its
modification time to determine if a target needs to be rebuilt. For
instance, in the above mmkfile, if the files `timefile` and `other` both
exist, but `timefile` is newer than `other`, `other` will be rebuilt.
Likewise, if `other` exists, but `timefile` does not, mmk will rebuild
timefile, notice it's newer than `other` and then rebuild `other`.

#### Regular-Expression Matching for Targets

mmk targets can be specified with regular expressions. The variable
`$target` is always set to the target name, and subgroup matches are
captured and assigned to variables named `match_[0-9]+`

Regular expression targets need to be defined inside single-quotes.

For example, the following rule will build any target matching the regular
expression `rule\.([0-9]+)' and the integer after the dot will be captured
into the `$match_1` variable.
```mmkfile
'myrule\.([0-9]+)' : 
	echo "executing rule $target (number $match_1)"
```
```
$ mmk myrule.1234
12:34:56 Starting myrule.1234
12:34:57 Building myrule.1234
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
13:55:04 Starting myrule.1234
13:55:04 Building myrule.1234
executing generic myrule target for myrule number 1234
$ mmk -v myrule.123
13:55:07 Starting myrule.123
13:55:07 Building myrule.123
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
13:45:56 Starting foo:mytype
13:45:56 Building foo:mytype
+ echo foo
$ mmk -v foo:clean
13:46:19 Starting foo:clean
13:46:19 Building foo:clean
+ rm foo
```

When executing a target without specifying the rule type, the first rule is chosen:
```
$ mmk -v foo
13:48:36 Starting foo
13:48:36 Building foo:mytype
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
14:35:52 Starting foo.123
14:35:52 Building foo.123
Putting a date into foo.123
$ mmk -v foo.456
14:35:57 Starting foo.456
14:35:57 Building foo.456
Putting a random number into foo.456
$ mmk -v foo.123:clean
14:36:04 Starting foo.123:clean
14:36:04 Building foo.123:clean
Cleaning foo.123
$ mmk -v foo.456:clean
14:36:09 Starting foo.456:clean
14:36:09 Building foo.456:clean
Cleaning foo.456
```

### Build Date Rule

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

#### Rule Type Definitions

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

We can then create targets of this type, which will exhibit the default behavior:
```
myfile : foo :
myotherfile : foo :
```

```
$ mmk -v myfile
14:53:23 Starting myfile
14:53:23 Building myfile:foo
Creating myfile, which is a foo.
$ cat myfile
foo
$ mmk -v myotherfile
14:53:32 Starting myotherfile
14:53:32 Building myotherfile:foo
Creating myotherfile, which is a foo.
$ cat myotherfile
foo
$ mmk -v myfile:clean
14:53:40 Starting myfile:clean
14:53:40 Building myfile:clean
Deleting myfile
$ mmk -v myotherfile:clean
14:53:47 Starting myotherfile:clean
14:53:47 Building myotherfile:clean
Deleting myotherfile
```
