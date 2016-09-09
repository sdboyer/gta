# gta

_gotta test 'em all!_

gta is a tool that helps you check your Go project against various versions of
its dependencies. This includes optionally running a command, like `go test`,
against each combination. So it's kinda like
[git-bisect](https://git-scm.com/docs/git-bisect), but for your deps.

gta is [gps](https://github.com/sdboyer/gps)-based, and should interoperate with
several different Go package managers (gb, godep, gom, gpm, and glide, though
glide is likely to work best). When available, gta reads the lock file from that
tool, and tries to hold constant all the versions listed there, varying only the
versions of the dependency you name.

In general, gta is probably most useful for exploring a semver range you've
already set in a manifest, or for simply exploring _all_ the possible versions
of a particular dependency.

## Installation

```
$ go get github.com/sdboyer/gta
```

## Usage

```
# For now, gta must be run from a project root
$ cd $GOPATH/src/github.com/me/myproject
# Just look for valid dependency solutions for each version of the dep
$ gta github.com/somedep/tocheckitsversions
# Or, also run "go test" against each version where there's a solution
$ gta -r "go test" github.com/somedep/tocheckitsversions
```

See `gta --help` for more information.

## Caveats

* This is very alpha - "release early, release often" - and while it has worked
  at least once, it will likely break :) Please file issues!
* The format of gta's `glide.yaml` file is not one your standard version of
  glide can read, so don't bother trying (it's based on the [PR to integrate
  gps into glide](https://github.com/Masterminds/glide/pull/384))
