// Command gen generates N synthetic Go application modules under an output
// directory. Every app is its own Go module with a seeded-random mix of
// third-party dependencies and a handful of internal packages whose source
// changes with the seed. This is meant to simulate many developers opening
// PRs against many Go services, so that build/module caches see realistic churn.
//
// Usage:
//
//	go run ./tools/gen -n 100 -seed 42 -out apps [-tidy=false]
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type dep struct {
	path, version string
	// imports is the import path used in main.go; usage is a code snippet
	// that references the package so the import is not unused.
	imports string
	usage   string
}

// A pool of real, commonly used modules with pinned versions. Each app picks
// a seeded subset so the module cache gets a mix of overlapping and unique
// downloads across apps.
var depPool = []dep{
	{"github.com/google/uuid", "v1.6.0", "github.com/google/uuid", `_ = uuid.New().String()`},
	{"go.uber.org/zap", "v1.27.0", "go.uber.org/zap", `l, _ := zap.NewProduction(); defer l.Sync(); l.Info("boot")`},
	{"github.com/spf13/cobra", "v1.8.1", "github.com/spf13/cobra", `_ = &cobra.Command{Use: "app"}`},
	{"github.com/spf13/pflag", "v1.0.5", "github.com/spf13/pflag", `_ = pflag.NewFlagSet("app", pflag.ContinueOnError)`},
	{"github.com/gin-gonic/gin", "v1.10.0", "github.com/gin-gonic/gin", `_ = gin.New()`},
	{"github.com/gorilla/mux", "v1.8.1", "github.com/gorilla/mux", `_ = mux.NewRouter()`},
	{"github.com/go-chi/chi/v5", "v5.1.0", "github.com/go-chi/chi/v5", `_ = chi.NewRouter()`},
	{"golang.org/x/sync", "v0.10.0", "golang.org/x/sync/errgroup", `var g errgroup.Group; _ = g.Wait()`},
	{"golang.org/x/text", "v0.21.0", "golang.org/x/text/cases", `_ = cases.Title`},
	{"gopkg.in/yaml.v3", "v3.0.1", "gopkg.in/yaml.v3", `_, _ = yaml.Marshal(map[string]int{"a": 1})`},
	{"github.com/pelletier/go-toml/v2", "v2.2.3", "github.com/pelletier/go-toml/v2", `_, _ = toml.Marshal(map[string]int{"a": 1})`},
	{"github.com/stretchr/testify", "v1.10.0", "github.com/stretchr/testify/assert", `_ = assert.Equal`},
	{"github.com/prometheus/client_golang", "v1.20.5", "github.com/prometheus/client_golang/prometheus", `_ = prometheus.NewCounter(prometheus.CounterOpts{Name: "x"})`},
	{"github.com/rs/zerolog", "v1.33.0", "github.com/rs/zerolog", `_ = zerolog.New(os.Stdout)`},
	{"github.com/sirupsen/logrus", "v1.9.3", "github.com/sirupsen/logrus", `logrus.SetLevel(logrus.InfoLevel)`},
	{"github.com/jackc/pgx/v5", "v5.7.1", "github.com/jackc/pgx/v5/pgtype", `_ = pgtype.Text{}`},
	{"github.com/redis/go-redis/v9", "v9.7.0", "github.com/redis/go-redis/v9", `_ = redis.NewClient(&redis.Options{})`},
	{"github.com/aws/aws-sdk-go-v2", "v1.32.6", "github.com/aws/aws-sdk-go-v2/aws", `_ = aws.String("x")`},
	{"google.golang.org/protobuf", "v1.36.0", "google.golang.org/protobuf/proto", `_ = proto.Marshal`},
	{"google.golang.org/grpc", "v1.69.2", "google.golang.org/grpc", `_ = grpc.NewServer()`},
	{"github.com/hashicorp/go-multierror", "v1.1.1", "github.com/hashicorp/go-multierror", `_ = multierror.Append(nil, nil)`},
	{"github.com/cespare/xxhash/v2", "v2.3.0", "github.com/cespare/xxhash/v2", `_ = xxhash.Sum64String("x")`},
	{"github.com/mitchellh/mapstructure", "v1.5.0", "github.com/mitchellh/mapstructure", `_ = mapstructure.Decode`},
	{"github.com/json-iterator/go", "v1.1.12", "github.com/json-iterator/go", `_ = jsoniter.ConfigFastest`},
	{"github.com/klauspost/compress", "v1.17.11", "github.com/klauspost/compress/zstd", `_, _ = zstd.NewWriter(nil)`},
}

func main() {
	n := flag.Int("n", 100, "number of apps to generate")
	seed := flag.Int64("seed", 1, "random seed (change to simulate a new round of PRs)")
	out := flag.String("out", "apps", "output directory")
	tidy := flag.Bool("tidy", true, "run 'go mod tidy' in each generated app (needs network)")
	pkgsPer := flag.Int("pkgs", 4, "internal packages per app")
	filesPer := flag.Int("files", 3, "files per internal package")
	funcsPer := flag.Int("funcs", 25, "functions per file")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	rng := rand.New(rand.NewSource(*seed))
	for i := 0; i < *n; i++ {
		name := fmt.Sprintf("app-%03d", i)
		dir := filepath.Join(*out, name)
		// Per-app RNG derived from the global one so app K is stable for a
		// given seed regardless of -n.
		appRng := rand.New(rand.NewSource(rng.Int63()))
		if err := genApp(dir, name, appRng, *pkgsPer, *filesPer, *funcsPer); err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		if *tidy {
			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = dir
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				log.Fatalf("%s: go mod tidy: %v", name, err)
			}
		}
		fmt.Printf("generated %s\n", dir)
	}
}

func genApp(dir, name string, rng *rand.Rand, pkgs, files, funcs int) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	modPath := "example.com/" + name

	// Pick 2..7 deps.
	perm := rng.Perm(len(depPool))
	deps := make([]dep, 0)
	for _, idx := range perm[:2+rng.Intn(6)] {
		deps = append(deps, depPool[idx])
	}

	var gomod strings.Builder
	fmt.Fprintf(&gomod, "module %s\n\ngo 1.24\n\nrequire (\n", modPath)
	for _, d := range deps {
		fmt.Fprintf(&gomod, "\t%s %s\n", d.path, d.version)
	}
	gomod.WriteString(")\n")
	if err := write(filepath.Join(dir, "go.mod"), gomod.String()); err != nil {
		return err
	}

	// Internal packages.
	pkgNames := make([]string, pkgs)
	for p := 0; p < pkgs; p++ {
		pkgNames[p] = fmt.Sprintf("pkg%d", p)
		pdir := filepath.Join(dir, "internal", pkgNames[p])
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			return err
		}
		for f := 0; f < files; f++ {
			src := genFile(pkgNames[p], f, funcs, rng)
			if err := write(filepath.Join(pdir, fmt.Sprintf("file%d.go", f)), src); err != nil {
				return err
			}
		}
		if err := write(filepath.Join(pdir, "pkg_test.go"), genTest(pkgNames[p], rng)); err != nil {
			return err
		}
	}

	// main.go
	var m strings.Builder
	m.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n")
	for _, p := range pkgNames {
		fmt.Fprintf(&m, "\t\"%s/internal/%s\"\n", modPath, p)
	}
	for _, d := range deps {
		fmt.Fprintf(&m, "\t\"%s\"\n", d.imports)
	}
	m.WriteString(")\n\nfunc main() {\n\t_ = os.Args\n")
	for _, d := range deps {
		fmt.Fprintf(&m, "\t{ %s }\n", d.usage)
	}
	m.WriteString("\tvar acc int64\n")
	for _, p := range pkgNames {
		fmt.Fprintf(&m, "\tacc += %s.Run(%d)\n", p, rng.Intn(1000))
	}
	fmt.Fprintf(&m, "\tfmt.Println(%q, acc)\n}\n", name)
	if err := write(filepath.Join(dir, "main.go"), m.String()); err != nil {
		return err
	}
	return nil
}

func genFile(pkg string, idx, funcs int, rng *rand.Rand) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by tools/gen; DO NOT EDIT.\n\npackage %s\n\nimport (\n\t\"strings\"\n\t\"strconv\"\n)\n\n", pkg)
	fmt.Fprintf(&b, "var _ = strings.ToUpper\nvar _ = strconv.Itoa\n\n")
	if idx == 0 {
		// Run fans out across every function in this package via a table.
		b.WriteString("var table []func(int64) int64\n\n")
		b.WriteString("// Run executes every generated function once, chaining results.\nfunc Run(seed int) int64 {\n\tv := int64(seed)\n\tfor _, f := range table {\n\t\tv = f(v)\n\t}\n\treturn v\n}\n\n")
	}
	fmt.Fprintf(&b, "func init() {\n\ttable = append(table,\n")
	for f := 0; f < funcs; f++ {
		fmt.Fprintf(&b, "\t\tF%d_%d,\n", idx, f)
	}
	b.WriteString("\t)\n}\n\n")
	for f := 0; f < funcs; f++ {
		fmt.Fprintf(&b, "// F%d_%d is a generated function.\nfunc F%d_%d(x int64) int64 {\n", idx, f, idx, f)
		stmts := 3 + rng.Intn(8)
		for s := 0; s < stmts; s++ {
			b.WriteString("\t" + genStmt(rng) + "\n")
		}
		b.WriteString("\treturn x\n}\n\n")
	}
	return b.String()
}

func genStmt(rng *rand.Rand) string {
	c := rng.Int63n(1_000_003) + 1
	switch rng.Intn(9) {
	case 0:
		return fmt.Sprintf("x = x*%d + %d", c%97+1, c)
	case 1:
		return fmt.Sprintf("x ^= %d", c)
	case 2:
		return fmt.Sprintf("if x%%%d == 0 { x += %d } else { x -= %d }", c%13+2, c, c/3)
	case 3:
		return fmt.Sprintf("for i := 0; i < %d; i++ { x += int64(i) * %d }", c%7+1, c%11)
	case 4:
		return fmt.Sprintf("x += int64(len(strconv.FormatInt(x, %d)))", 2+int(c%35))
	case 5:
		return fmt.Sprintf("x += int64(strings.Count(strconv.FormatInt(x, 10), %q))", strconv(c%10))
	case 6:
		return fmt.Sprintf("x = (x << %d) | (x >> %d)", c%31+1, 63-c%31)
	case 7:
		return fmt.Sprintf("{ s := []int64{%d, %d, %d}; for _, v := range s { x += v } }", c, c/2, c/5)
	default:
		return fmt.Sprintf("x -= %d", c)
	}
}

func strconv(d int64) string { return fmt.Sprintf("%d", d) }

func genTest(pkg string, rng *rand.Rand) string {
	return fmt.Sprintf(`package %s

import "testing"

func TestRun(t *testing.T) {
	a, b := Run(%d), Run(%d)
	if a == 0 && b == 0 {
		t.Log("both zero, fine")
	}
}
`, pkg, rng.Intn(100), rng.Intn(100))
}

func write(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
