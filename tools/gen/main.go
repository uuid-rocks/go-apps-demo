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
	"runtime"
	"strings"
	"sync"
)

type dep struct {
	path     string
	versions []string // one is picked per app, so different seeds pull different versions
	imports  string   // import path used in main.go
	alias    string   // optional import alias (avoids package-name collisions)
	usage    string   // snippet referencing the package so the import is used
}

// A pool of real modules with several pinned versions each. Each app picks a
// seeded subset and a seeded version of each, so the module cache sees both
// overlap and lots of unique downloads across apps and across seeds. The
// "heavy" entries at the bottom pull in very large dependency trees.
var depPool = []dep{
	{"github.com/google/uuid", []string{"v1.3.1", "v1.4.0", "v1.5.0", "v1.6.0"}, "github.com/google/uuid", "", `_ = uuid.New().String()`},
	{"go.uber.org/zap", []string{"v1.24.0", "v1.25.0", "v1.26.0", "v1.27.0"}, "go.uber.org/zap", "", `l, _ := zap.NewProduction(); defer l.Sync(); l.Info("boot")`},
	{"github.com/spf13/cobra", []string{"v1.6.1", "v1.7.0", "v1.8.0", "v1.8.1"}, "github.com/spf13/cobra", "", `_ = &cobra.Command{Use: "app"}`},
	{"github.com/spf13/viper", []string{"v1.17.0", "v1.18.2", "v1.19.0"}, "github.com/spf13/viper", "", `_ = viper.New()`},
	{"github.com/urfave/cli/v2", []string{"v2.25.7", "v2.27.1", "v2.27.5"}, "github.com/urfave/cli/v2", "", `_ = &cli.App{}`},
	{"github.com/gin-gonic/gin", []string{"v1.9.0", "v1.9.1", "v1.10.0"}, "github.com/gin-gonic/gin", "", `_ = gin.New()`},
	{"github.com/gorilla/mux", []string{"v1.8.0", "v1.8.1"}, "github.com/gorilla/mux", "", `_ = mux.NewRouter()`},
	{"github.com/go-chi/chi/v5", []string{"v5.0.10", "v5.0.12", "v5.1.0"}, "github.com/go-chi/chi/v5", "", `_ = chi.NewRouter()`},
	{"github.com/labstack/echo/v4", []string{"v4.11.4", "v4.12.0"}, "github.com/labstack/echo/v4", "", `_ = echo.New()`},
	{"github.com/gofiber/fiber/v2", []string{"v2.51.0", "v2.52.5"}, "github.com/gofiber/fiber/v2", "", `_ = fiber.New()`},
	{"golang.org/x/sync", []string{"v0.5.0", "v0.6.0", "v0.7.0", "v0.8.0", "v0.10.0"}, "golang.org/x/sync/errgroup", "", `var g errgroup.Group; _ = g.Wait()`},
	{"golang.org/x/text", []string{"v0.14.0", "v0.16.0", "v0.18.0", "v0.21.0"}, "golang.org/x/text/cases", "", `_ = cases.Title`},
	{"golang.org/x/crypto", []string{"v0.17.0", "v0.21.0", "v0.28.0", "v0.31.0"}, "golang.org/x/crypto/bcrypt", "", `_ = bcrypt.DefaultCost`},
	{"golang.org/x/net", []string{"v0.19.0", "v0.23.0", "v0.28.0", "v0.33.0"}, "golang.org/x/net/html", "", `_ = html.EscapeString`},
	{"gopkg.in/yaml.v3", []string{"v3.0.1"}, "gopkg.in/yaml.v3", "", `_, _ = yaml.Marshal(map[string]int{"a": 1})`},
	{"github.com/pelletier/go-toml/v2", []string{"v2.1.1", "v2.2.3"}, "github.com/pelletier/go-toml/v2", "", `_, _ = toml.Marshal(map[string]int{"a": 1})`},
	{"github.com/stretchr/testify", []string{"v1.8.4", "v1.9.0", "v1.10.0"}, "github.com/stretchr/testify/assert", "", `_ = assert.Equal`},
	{"github.com/prometheus/client_golang", []string{"v1.17.0", "v1.18.0", "v1.19.1", "v1.20.5"}, "github.com/prometheus/client_golang/prometheus", "", `_ = prometheus.NewCounter(prometheus.CounterOpts{Name: "x"})`},
	{"go.opentelemetry.io/otel", []string{"v1.21.0", "v1.24.0", "v1.28.0", "v1.32.0"}, "go.opentelemetry.io/otel", "", `_ = otel.Tracer("x")`},
	{"github.com/rs/zerolog", []string{"v1.31.0", "v1.32.0", "v1.33.0"}, "github.com/rs/zerolog", "", `_ = zerolog.New(os.Stdout)`},
	{"github.com/sirupsen/logrus", []string{"v1.9.3"}, "github.com/sirupsen/logrus", "", `logrus.SetLevel(logrus.InfoLevel)`},
	{"github.com/jackc/pgx/v5", []string{"v5.5.0", "v5.6.0", "v5.7.1"}, "github.com/jackc/pgx/v5/pgtype", "", `_ = pgtype.Text{}`},
	{"github.com/jmoiron/sqlx", []string{"v1.3.5", "v1.4.0"}, "github.com/jmoiron/sqlx", "", `_ = sqlx.Open`},
	{"gorm.io/gorm", []string{"v1.25.5", "v1.25.12"}, "gorm.io/gorm", "", `_ = &gorm.Config{}`},
	{"github.com/redis/go-redis/v9", []string{"v9.3.1", "v9.5.1", "v9.7.0"}, "github.com/redis/go-redis/v9", "", `_ = redis.NewClient(&redis.Options{})`},
	{"github.com/nats-io/nats.go", []string{"v1.31.0", "v1.34.1", "v1.37.0"}, "github.com/nats-io/nats.go", "", `_ = nats.DefaultURL`},
	{"github.com/segmentio/kafka-go", []string{"v0.4.45", "v0.4.47"}, "github.com/segmentio/kafka-go", "", `_ = kafka.Message{}`},
	{"github.com/elastic/go-elasticsearch/v8", []string{"v8.11.1", "v8.15.0"}, "github.com/elastic/go-elasticsearch/v8", "", `_ = elasticsearch.NewDefaultClient`},
	{"google.golang.org/protobuf", []string{"v1.32.0", "v1.33.0", "v1.34.2", "v1.36.0"}, "google.golang.org/protobuf/proto", "", `_ = proto.Marshal`},
	{"google.golang.org/grpc", []string{"v1.60.1", "v1.62.1", "v1.65.0", "v1.69.2"}, "google.golang.org/grpc", "", `_ = grpc.NewServer()`},
	{"github.com/grpc-ecosystem/grpc-gateway/v2", []string{"v2.19.0", "v2.22.0", "v2.24.0"}, "github.com/grpc-ecosystem/grpc-gateway/v2/runtime", "gwruntime", `_ = gwruntime.NewServeMux()`},
	{"github.com/golang-jwt/jwt/v5", []string{"v5.2.0", "v5.2.1"}, "github.com/golang-jwt/jwt/v5", "", `_ = jwt.New(jwt.SigningMethodHS256)`},
	{"github.com/go-playground/validator/v10", []string{"v10.16.0", "v10.19.0", "v10.23.0"}, "github.com/go-playground/validator/v10", "", `_ = validator.New()`},
	{"github.com/hashicorp/go-multierror", []string{"v1.1.1"}, "github.com/hashicorp/go-multierror", "", `_ = multierror.Append(nil, nil)`},
	{"github.com/cespare/xxhash/v2", []string{"v2.2.0", "v2.3.0"}, "github.com/cespare/xxhash/v2", "", `_ = xxhash.Sum64String("x")`},
	{"github.com/mitchellh/mapstructure", []string{"v1.5.0"}, "github.com/mitchellh/mapstructure", "", `_ = mapstructure.Decode`},
	{"github.com/json-iterator/go", []string{"v1.1.12"}, "github.com/json-iterator/go", "", `_ = jsoniter.ConfigFastest`},
	{"github.com/klauspost/compress", []string{"v1.17.4", "v1.17.9", "v1.17.11"}, "github.com/klauspost/compress/zstd", "", `_, _ = zstd.NewWriter(nil)`},
	// Heavy: huge module downloads and/or large compile trees.
	{"github.com/aws/aws-sdk-go", []string{"v1.48.0", "v1.50.0", "v1.55.5"}, "github.com/aws/aws-sdk-go/aws", "awsv1", `_ = awsv1.String("x")`},
	{"github.com/aws/aws-sdk-go-v2", []string{"v1.24.0", "v1.27.0", "v1.30.0", "v1.32.6"}, "github.com/aws/aws-sdk-go-v2/aws", "", `_ = aws.String("x")`},
	{"github.com/aws/aws-sdk-go-v2/service/s3", []string{"v1.48.0", "v1.55.0", "v1.60.0", "v1.70.0"}, "github.com/aws/aws-sdk-go-v2/service/s3", "", `_ = s3.NewFromConfig`},
	{"github.com/aws/aws-sdk-go-v2/service/dynamodb", []string{"v1.27.0", "v1.32.0", "v1.36.0"}, "github.com/aws/aws-sdk-go-v2/service/dynamodb", "", `_ = dynamodb.NewFromConfig`},
	{"cloud.google.com/go/storage", []string{"v1.36.0", "v1.40.0", "v1.43.0"}, "cloud.google.com/go/storage", "gcs", `_ = gcs.NewClient`},
	{"k8s.io/client-go", []string{"v0.29.0", "v0.30.0", "v0.31.0"}, "k8s.io/client-go/kubernetes", "", `_ = kubernetes.NewForConfigOrDie`},
	{"github.com/aws/aws-sdk-go-v2/service/ec2", []string{"v1.140.0", "v1.160.0", "v1.190.0"}, "github.com/aws/aws-sdk-go-v2/service/ec2", "", `_ = ec2.NewFromConfig`},
	{"github.com/minio/minio-go/v7", []string{"v7.0.63", "v7.0.70", "v7.0.80"}, "github.com/minio/minio-go/v7", "minio", `_ = minio.New`},
	{"github.com/hashicorp/terraform-plugin-sdk/v2", []string{"v2.30.0", "v2.34.0"}, "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema", "tfschema", `_ = &tfschema.Resource{}`},
}

func main() {
	n := flag.Int("n", 100, "number of apps to generate")
	seed := flag.Int64("seed", 1, "random seed (change to simulate a new round of PRs)")
	out := flag.String("out", "apps", "output directory")
	tidy := flag.Bool("tidy", true, "run 'go mod tidy' in each generated app (needs network)")
	pkgsPer := flag.Int("pkgs", 12, "internal packages per app")
	filesPer := flag.Int("files", 6, "files per internal package")
	funcsPer := flag.Int("funcs", 40, "functions per file")
	only := flag.Int("only", -1, "generate only app with this index (0-based); output is identical to a full run")
	jobs := flag.Int("j", runtime.NumCPU(), "parallel 'go mod tidy' workers")
	flag.Parse()
	if *n < 1 {
		log.Fatal("-n must be >= 1")
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	rng := rand.New(rand.NewSource(*seed))
	sem := make(chan struct{}, max(*jobs, 1))
	var wg sync.WaitGroup
	for i := 0; i < *n; i++ {
		name := fmt.Sprintf("app-%03d", i)
		dir := filepath.Join(*out, name)
		// Per-app RNG derived from the global one so app K is stable for a
		// given seed regardless of -n.
		appRng := rand.New(rand.NewSource(rng.Int63()))
		if *only >= 0 && i != *only {
			continue
		}
		// Source generation is deterministic and cheap; do it inline.
		if err := genApp(dir, name, appRng, *pkgsPer, *filesPer, *funcsPer); err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		if !*tidy {
			fmt.Printf("generated %s\n", dir)
			continue
		}
		// go mod tidy is network-bound; run those concurrently.
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = dir
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				log.Fatalf("%s: go mod tidy: %v", name, err)
			}
			fmt.Printf("generated %s\n", dir)
		}()
	}
	wg.Wait()
}

func genApp(dir, name string, rng *rand.Rand, pkgs, files, funcs int) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	modPath := "example.com/" + name

	// Pick 5..12 deps, each at a seeded version.
	perm := rng.Perm(len(depPool))
	deps := make([]dep, 0)
	for _, idx := range perm[:5+rng.Intn(8)] {
		d := depPool[idx]
		d.versions = []string{d.versions[rng.Intn(len(d.versions))]}
		deps = append(deps, d)
	}

	var gomod strings.Builder
	fmt.Fprintf(&gomod, "module %s\n\ngo 1.24\n\nrequire (\n", modPath)
	for _, d := range deps {
		fmt.Fprintf(&gomod, "\t%s %s\n", d.path, d.versions[0])
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
		if d.alias != "" {
			fmt.Fprintf(&m, "\t%s \"%s\"\n", d.alias, d.imports)
		} else {
			fmt.Fprintf(&m, "\t\"%s\"\n", d.imports)
		}
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
