package main

import (
	"fmt"
	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Masterminds/glide/dependency"
	gpath "github.com/Masterminds/glide/path"
	"github.com/sdboyer/gps"
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "gta",
	Short: "Ensure that builds work across sets of acceptable dependency versions",
	Long: `gta (gotta test 'em all!') ensures that a build works across ranges of possible
versions for its dependencies.

For example, if your project depends on github.com/foo/bar, and three versions
of that repository exist, then gta can be used to determine if your build will
"work" for each of those versions:

$ gta github.com/foo/bar

By default, gta will simply determine if a dependency solution exists that's
viable for each dep version. However, if a value is passed for --run, then
gta will also execute that command for each solution. ` + "`go test`" + ` is usually
the simplest useful command to run here.

Unless --no-pm is specified, gta will try to detect if metadata files for
package managers (currently only glide) are present. If so, rather than testing
all possible versions of the dependency, it will only check versions that are
allowed by the constraints specified in those files.`,
	RunE: RunGTA,
}

var (
	run                     string
	branch, semver, version string
	verbose                 bool
)

func main() {
	// 1. write basic command, absent manifest/lock loading
	// 2. write support for executing e.g. go test
	// 3. loader for glide files
	RootCmd.Flags().StringVarP(&run, "run", "r", "", "Additional command to run (e.g. `go test`) as a check")
	RootCmd.Flags().StringVar(&semver, "semver", "", "Semantic version (range or single version) to check")
	RootCmd.Flags().StringVar(&branch, "branch", "", "Branch to check")
	RootCmd.Flags().StringVar(&version, "version", "", "Version (non-semver tag) to check")
	RootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func RunGTA(cmd *cobra.Command, args []string) error {
	// Turn off errors, now that we're in here
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var pkg string
	switch len(args) {
	case 1:
		pkg = args[0]
		break
	default:
		return fmt.Errorf("You must specify a single dependency to check against its versions.\n")
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("Could not get working directory: %s", err)
	}

	an := dependency.Analyzer{}
	sm, err := gps.NewSourceManager(an, filepath.Join(gpath.Home(), "cache"), false)
	if err != nil {
		return fmt.Errorf("Failed to set up SourceManager: %s", err)
	}
	defer sm.Release()

	root, err := sm.DeduceProjectRoot(pkg)
	if err != nil {
		return fmt.Errorf("Could not detect source info for %s: %s", pkg, err)
	}

	pi := gps.ProjectIdentifier{
		ProjectRoot: root,
	}
	vlist, err := sm.ListVersions(pi)
	if err != nil {
		return fmt.Errorf("Could not retrieve version list for %s: %s", pi, err)
	}

	if len(vlist) == 0 {
		// shouldn't be possible, but whatever
		return fmt.Errorf("No versions could be located for %s", pi)
	}

	gps.SortForUpgrade(vlist)

	// obnoxious constraint parsing
	var c gps.Constraint
	switch {
	case branch == "" && semver == "" && version == "":
		c = gps.Any()
	case branch != "":
		if semver != "" || version != "" {
			return fmt.Errorf("Please specify only one type of constraint - branch, version, or semver")
		}
		c = gps.NewBranch(branch)
	case version != "":
		if semver != "" || branch != "" {
			return fmt.Errorf("Please specify only one type of constraint - branch, version, or semver")
		}
		c = gps.NewVersion(version)
	case semver != "":
		if version != "" || branch != "" {
			return fmt.Errorf("Please specify only one type of constraint - branch, version, or semver")
		}
		c, err = gps.NewSemverConstraint(semver)
		if err != nil {
			return fmt.Errorf("%s is not a valid semver constraint", semver)
		}
	}

	// Assume the current directory is correctly placed on a GOPATH, and derive
	// the ProjectRoot from it
	srcprefix := filepath.Join(build.Default.GOPATH, "src") + string(filepath.Separator)
	importroot := filepath.ToSlash(strings.TrimPrefix(wd, srcprefix))

	// Use the analyzer to figure out this project, too
	m, l, err := an.DeriveManifestAndLock(wd, gps.ProjectRoot(importroot))
	if err != nil {
		return fmt.Errorf("Error on trying to read project manifest and lock: %s", err)
	}
	rm := prepManifest(m)

	//pretty.Println(m, rm, l)

	var focus gps.ProjectConstraint
	var has bool
	if focus, has = rm.c[root]; !has {
		focus = gps.ProjectConstraint{
			Ident: gps.ProjectIdentifier{
				ProjectRoot: root,
			},
		}
	}

	focus.Constraint = c

	// Set up params, including tracing
	params := gps.SolveParameters{
		Manifest:    rm,
		Lock:        l,
		RootDir:     wd,
		ImportRoot:  gps.ProjectRoot(importroot),
		Trace:       true,
		TraceLogger: log.New(os.Stdout, "", 0),
	}

	var vl []gps.Version
	for _, v := range vlist {
		if focus.Constraint.Matches(v) {
			vl = append(vl, v)
		}
	}

	if len(vl) == 0 {
		return fmt.Errorf("%s has %v versions, but none matched constraint %s", root, len(vlist), c)
	}

	fmt.Printf("Checking %s with the following versions:\n\t%s\n", root, vl)

	type solnOrErr struct {
		v   gps.Version
		s   gps.Solution
		err error
	}

	ppi := func(id gps.ProjectIdentifier) string {
		if id.NetworkName == "" || id.NetworkName == string(id.ProjectRoot) {
			return string(id.ProjectRoot)
		}
		return fmt.Sprintf("%s (from %s)", id.ProjectRoot, id.NetworkName)
	}

	solns := make([]solnOrErr, len(vl))
	for k, v := range vl {
		fmt.Printf("Looking for solution with %s@%s...", root, v)
		focus.Constraint = v
		rm.c[root] = focus

		// TODO parallel, bwahaha
		soe := solnOrErr{v: v}
		// TODO reparse root project every time...horribly wasteful
		var s gps.Solver
		s, soe.err = gps.Prepare(params, sm)
		if soe.err == nil {
			soe.s, soe.err = s.Solve()
		}

		if soe.err == nil {
			fmt.Println("success!")
			if verbose {
				for _, p := range soe.s.Projects() {
					id := p.Ident()
					switch v := p.Version().(type) {
					case gps.Revision:
						fmt.Printf("\t%s at %s", ppi(id), v.String()[:7])
					case gps.UnpairedVersion:
						fmt.Printf("\t%s at %s", ppi(id), v)
					case gps.PairedVersion:
						fmt.Printf("\t%s at %s (%s)", ppi(id), v, v.Underlying().String()[:7])
					}
				}
			}
		} else {
			fmt.Println("failed.")
			if verbose {
				fmt.Println(soe.err)
			}
		}
		solns[k] = soe
	}
	fmt.Println("") // just a spacer

	// If we have to create these vendor trees, then back up the original vendor
	vpath := filepath.Join(wd, "vendor")
	var fail bool
	if run != "" {
		if _, err = os.Stat(vpath); err == nil {
			err = os.Rename(vpath, filepath.Join(wd, "_origvendor"))
			if err != nil {
				return fmt.Errorf("Failed to back up vendor folder: %s", err)
			}
			defer os.Rename(filepath.Join(wd, "_origvendor"), vpath)
		}
	}

	for _, soln := range solns {
		nv := fmt.Sprintf("%s@%s", root, soln.v)
		// If solving failed, no point in even checking the run
		if soln.err != nil {
			fail = true
			fmt.Printf("%s failed solving: %s\n", nv, soln.err)
			continue
		}

		if run == "" {
			fmt.Printf("%s succeeded\n", nv)
		} else {
			err = gps.WriteDepTree(vpath, soln.s, sm, true)
			if err != nil {
				fail = true
				fmt.Printf("skipping check: could not write tree for %s (err %s)\n", nv, err)
				continue
			}

			parts := strings.Split(run, " ")
			scmd := exec.Command(parts[0], parts[1:]...)
			out, err := scmd.CombinedOutput()
			if err != nil {
				fail = true
				fmt.Printf("%s failed with %s, output:\n%s\n", nv, err, string(out))
			} else {
				fmt.Printf("%s succeeded\n", nv)
			}

			os.RemoveAll(vpath)
			//os.Rename(vpath, filepath.Join(wd, "vend-"+soln.v.String()))
		}
	}

	if fail {
		return fmt.Errorf("Encountered one or more errors")
	}

	return nil
}

type simpleRootManifest struct {
	c   map[gps.ProjectRoot]gps.ProjectConstraint
	tc  map[gps.ProjectRoot]gps.ProjectConstraint
	ovr gps.ProjectConstraints
	ig  map[string]bool
}

func (m simpleRootManifest) DependencyConstraints() []gps.ProjectConstraint {
	ds := make([]gps.ProjectConstraint, 0)
	for _, d := range m.c {
		ds = append(ds, d)
	}
	return ds
}

func (m simpleRootManifest) TestDependencyConstraints() []gps.ProjectConstraint {
	ds := make([]gps.ProjectConstraint, 0)
	for _, d := range m.tc {
		ds = append(ds, d)
	}
	return ds
}

func (m simpleRootManifest) Overrides() gps.ProjectConstraints {
	return m.ovr
}

func (m simpleRootManifest) IgnorePackages() map[string]bool {
	return m.ig
}

func prepManifest(m gps.Manifest) simpleRootManifest {
	rm := simpleRootManifest{
		c:  make(map[gps.ProjectRoot]gps.ProjectConstraint),
		tc: make(map[gps.ProjectRoot]gps.ProjectConstraint),
	}

	if m == nil {
		return rm
	}

	for _, d := range m.DependencyConstraints() {
		rm.c[d.Ident.ProjectRoot] = d
	}
	for _, d := range m.TestDependencyConstraints() {
		rm.tc[d.Ident.ProjectRoot] = d
	}

	return rm
}
