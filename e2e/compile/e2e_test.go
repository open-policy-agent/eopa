//go:build e2e

package tests

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/go-cmp/cmp"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/open-policy-agent/opa/v1/server/types"

	"github.com/styrainc/enterprise-opa-private/e2e/utils"
	our_wait "github.com/styrainc/enterprise-opa-private/e2e/wait"
)

var eopaHTTPPort int

func TestMain(m *testing.M) {
	r := rand.New(rand.NewSource(2908))
	for {
		port := r.Intn(38181) + 1
		if utils.IsTCPPortBindable(port) {
			eopaHTTPPort = port
			break
		}
	}

	os.Exit(m.Run())
}

// This is temporary! The conversion logic is going to become part of the
// extended compile handler.
//
//go:embed convert.rego
var convertRego string

type DBType string

const (
	Postgres DBType = "postgres"
	MySQL    DBType = "mysql"
	MSSQL    DBType = "sqlserver"
)

// TestConfig holds test configuration
type TestConfig struct {
	db      *sql.DB
	dbName  string
	dbType  DBType
	baseURL string
}

// containerConfig holds database-specific container configuration
type containerConfig struct {
	image       string
	port        string
	env         map[string]string
	waitFor     wait.Strategy
	urlTemplate string
}

var dbConfigs = map[DBType]containerConfig{
	Postgres: {
		image: "postgres:17-alpine",
		port:  "5432/tcp",
		env: map[string]string{
			"POSTGRES_DB":       "testdb",
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
		},
		// waitFor:     wait.ForListeningPort("5432/tcp"),
		waitFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(15 * time.Second),
		urlTemplate: "postgres://testuser:testpass@%s:%s/testdb?sslmode=disable",
	},
	MySQL: {
		image: "mysql:9",
		port:  "3306/tcp",
		env: map[string]string{
			"MYSQL_DATABASE":      "testdb",
			"MYSQL_USER":          "testuser",
			"MYSQL_PASSWORD":      "testpass",
			"MYSQL_ROOT_PASSWORD": "rootpass",
		},
		waitFor:     wait.ForListeningPort("3306/tcp"),
		urlTemplate: "testuser:testpass@tcp(%s:%s)/testdb",
	},
	MSSQL: {
		image: "mcr.microsoft.com/mssql/server:2022-latest",
		port:  "1433/tcp",
		env: map[string]string{
			"ACCEPT_EULA":       "Y",
			"MSSQL_SA_PASSWORD": "MyStr0ngPassw0rd!",
		},
		waitFor:     wait.ForLog("Recovery is complete."),
		urlTemplate: "sqlserver://sa:MyStr0ngPassw0rd!@%s:%s",
	},
}

// setupTestContainer creates and starts a database container
func setupTestContainer(ctx context.Context, dbType DBType) (testcontainers.Container, string, error) {
	config := dbConfigs[dbType]

	containerReq := testcontainers.ContainerRequest{
		Image:        config.image,
		ExposedPorts: []string{config.port},
		WaitingFor:   config.waitFor,
		Env:          config.env,
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: containerReq,
		Started:          true,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to start container: %v", err)
	}

	port, err := container.MappedPort(ctx, nat.Port(config.port))
	if err != nil {
		return nil, "", fmt.Errorf("failed to get container port: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get container host: %v", err)
	}

	dbURL := fmt.Sprintf(config.urlTemplate, host, port.Port())
	return container, dbURL, nil
}

// getCreateTableSQL returns database-specific CREATE TABLE SQL
func getCreateTableSQL(dbType DBType) string {
	switch dbType {
	case Postgres:
		return `
        CREATE TABLE IF NOT EXISTS fruit (
            id SERIAL PRIMARY KEY,
            name VARCHAR(100) NOT NULL,
            colour VARCHAR(100) NOT NULL,
            price INT NOT NULL
        )`
	case MySQL:
		return `
        CREATE TABLE IF NOT EXISTS fruit (
            id INT AUTO_INCREMENT PRIMARY KEY,
            name VARCHAR(100) NOT NULL,
            colour VARCHAR(100) NOT NULL,
            price INT NOT NULL
        )`
	case MSSQL:
		return `
        IF NOT EXISTS (SELECT * FROM sysobjects WHERE name='fruit' AND xtype='U')
        CREATE TABLE fruit (
            id INT IDENTITY(1,1) PRIMARY KEY,
            name NVARCHAR(100) NOT NULL,
            colour NVARCHAR(100) NOT NULL,
            price INT NOT NULL
        )`
	}
	panic("unknown db type")
}

// initializeTestData sets up initial test data in the database
func initializeTestData(db *sql.DB, dbType DBType) error {
	createTableSQL := getCreateTableSQL(dbType)
	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create table: %v", err)
	}

	// Insert test data - using parameterized queries for better compatibility
	var insertDataSQL string
	switch dbType {
	case Postgres:
		insertDataSQL = "INSERT INTO fruit (name, colour, price) VALUES ($1, $2, $3)"
	case MSSQL:
		insertDataSQL = "INSERT INTO fruit (name, colour, price) VALUES (@p1, @p2, @p3)"
	default:
		insertDataSQL = "INSERT INTO fruit (name, colour, price) VALUES (?, ?, ?)"
	}

	for _, f := range []struct {
		name   string
		colour string
		price  int
	}{
		{"apple", "green", 10},
		{"banana", "yellow", 20},
		{"cherry", "red", 11},
	} {
		if _, err := db.Exec(insertDataSQL, f.name, f.colour, f.price); err != nil {
			return fmt.Errorf("failed to insert test data: %v", err)
		}
	}

	return nil
}

// setupDB initializes a test database of the specified type
func setupDB(t *testing.T, dbType DBType) (*TestConfig, func()) {
	t.Helper()
	ctx := context.Background()

	container, dbURL, err := setupTestContainer(ctx, dbType)
	if err != nil {
		t.Fatalf("failed to setup test container: %v", err)
	}

	db, err := sql.Open(string(dbType), dbURL)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	if err := initializeTestData(db, dbType); err != nil {
		t.Fatalf("failed to initialize test data: %v", err)
	}

	cleanup := func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close database connection: %v", err)
		}
		if err := container.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate container: %v", err)
		}
	}

	return &TestConfig{
		db:     db,
		dbName: "testdb",
		dbType: dbType,
	}, cleanup
}

type fruitRow struct {
	ID     int
	Name   string
	Colour string
	Price  int
}

func TestCompileHappyPathE2E(t *testing.T) {
	dbTypes := []DBType{Postgres, MySQL, MSSQL}

	policy := convertRego
	eopa, _, eopaErr := loadEnterpriseOPA(t, policy, eopaHTTPPort)
	if err := eopa.Start(); err != nil {
		t.Fatal(err)
	}
	our_wait.ForLog(t, eopaErr, func(s string) bool { return strings.Contains(s, "Server initialized") }, time.Second)
	eopaURL := fmt.Sprintf("http://localhost:%d", eopaHTTPPort)

	for _, dbType := range dbTypes {
		t.Run(string(dbType), func(t *testing.T) {
			config, cleanup := setupDB(t, dbType)
			defer t.Cleanup(cleanup)

			unknowns := []string{"input.fruits"}
			var input any = map[string]any{"fav_colour": "yellow"}
			query := `data.filters.include`
			selectQuery := "SELECT * FROM fruit"

			apple := fruitRow{ID: 1, Name: "apple", Colour: "green", Price: 10}
			banana := fruitRow{ID: 2, Name: "banana", Colour: "yellow", Price: 20}
			cherry := fruitRow{ID: 3, Name: "cherry", Colour: "red", Price: 11}

			tests := []struct {
				name    string
				policy  string
				dbType  DBType
				expRows []fruitRow
			}{
				{
					name:    "simple equality",
					policy:  `include if input.fruits.colour == input.fav_colour`,
					dbType:  dbType,
					expRows: []fruitRow{banana},
				},
				{
					name:    "simple comparison",
					policy:  `include if input.fruits.price < 11`,
					dbType:  dbType,
					expRows: []fruitRow{apple},
				},
				{
					name: "conjunct query, inequality",
					policy: `include if {
						input.fruits.name != "apple"
						input.fruits.name != "banana"
						}`,
					dbType:  dbType,
					expRows: []fruitRow{cherry},
				},
				{
					name: "disjunct query, equality",
					policy: `include if input.fruits.name == "apple"
						include if input.fruits.name == "banana"`,
					dbType:  dbType,
					expRows: []fruitRow{apple, banana},
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					{
						// first, we override the policy with the current test case
						policy := fmt.Sprintf("package filters\n%s", tt.policy)
						req, err := http.NewRequest("PUT", fmt.Sprintf("%s/v1/policies/policy.rego", eopaURL), strings.NewReader(policy))
						if err != nil {
							t.Fatalf("failed to create request: %v", err)
						}
						if _, err := http.DefaultClient.Do(req); err != nil {
							t.Fatalf("post policy: %v", err)
						}
					}

					var respPayload map[string]any
					{
						// second, query the compile API
						payload := types.CompileRequestV1{
							Input:    &input,
							Query:    query,
							Unknowns: &unknowns,
						}

						queryBytes, err := json.Marshal(payload)
						if err != nil {
							t.Fatalf("Failed to marshal JSON: %v", err)
						}

						req, err := http.NewRequest("POST",
							fmt.Sprintf("%s/exp/compile", eopaURL),
							strings.NewReader(string(queryBytes)))
						if err != nil {
							t.Fatalf("failed to create request: %v", err)
						}
						req.Header.Set("Content-Type", "application/json")

						resp, err := http.DefaultClient.Do(req)
						if err != nil {
							t.Fatalf("failed to execute request: %v", err)
						}
						defer resp.Body.Close()

						if status := resp.StatusCode; status != http.StatusOK {
							t.Errorf("expected status %v, got %v", http.StatusOK, status)
						}

						if err := json.NewDecoder(resp.Body).Decode(&respPayload); err != nil {
							t.Fatalf("failed to decode response: %v", err)
						}
					}

					var whereClauses string
					{
						// use the convert.rego policy to convert the Compile API resonse to WHERE clauses via UCAST
						convertPayload, err := json.Marshal(map[string]any{
							"input": map[string]any{
								"compile":      respPayload,
								"dialect":      string(tt.dbType),
								"replacements": map[string]any{"fruits": map[string]any{"$self": "fruit"}}},
						})
						if err != nil {
							t.Fatalf("failed to marshal convert payload: %v", err)
						}
						resp, err := http.Post(fmt.Sprintf("%s/v1/data/convert/converted", eopaURL), "application/json", bytes.NewReader(convertPayload))
						if err != nil {
							t.Fatalf("failed to convert: %v", err)
						}
						defer resp.Body.Close()

						if status := resp.StatusCode; status != http.StatusOK {
							t.Fatalf("expected status %v, got %v", http.StatusOK, status)
						}

						var respM map[string]any
						if err := json.NewDecoder(resp.Body).Decode(&respM); err != nil {
							t.Fatalf("unmarshal response: %v", err)
						}
						t.Logf("converted: %v", respM)
						whereClauses = respM["result"].(string)
					}

					var rowsData []fruitRow
					{
						// finally, query the database with the resulting WHERE clauses
						stmt := selectQuery + " " + whereClauses
						rows, err := config.db.Query(stmt)
						if err != nil {
							t.Fatalf("%s: error: %v", stmt, err)
						}
						// collect rows into rowsData
						for rows.Next() {
							var fruit fruitRow
							// scan row into fruit, ignoring created_at
							if err := rows.Scan(&fruit.ID, &fruit.Name, &fruit.Colour, &fruit.Price); err != nil {
								t.Fatalf("failed to scan row: %v", err)
							}
							rowsData = append(rowsData, fruit)
						}
					}

					{
						// finally, compare with expected!
						if diff := cmp.Diff(tt.expRows, rowsData); diff != "" {
							t.Errorf("unexpected result (-want +got):\n%s", diff)
						}
					}
				})
			}
		})
	}
}

func loadEnterpriseOPA(t *testing.T, policy string, httpPort int) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	stdout, stderr := bytes.Buffer{}, bytes.Buffer{}
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "eval.rego")
	if err := os.WriteFile(policyPath, []byte(policy), 0x777); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	args := []string{
		"run",
		"--server",
		"--addr", fmt.Sprintf("localhost:%d", httpPort),
		"--log-level=debug",
		"--disable-telemetry",
	}
	bin := os.Getenv("BINARY")
	if bin == "" {
		bin = "eopa"
	}
	eopa := exec.Command(bin, append(args, policyPath)...)
	eopa.Stderr = &stderr
	eopa.Stdout = &stdout
	eopa.Env = append(eopa.Environ(),
		"EOPA_LICENSE_TOKEN="+os.Getenv("EOPA_LICENSE_TOKEN"),
		"EOPA_LICENSE_KEY="+os.Getenv("EOPA_LICENSE_KEY"),
	)

	t.Cleanup(func() {
		if eopa.Process == nil {
			return
		}
		if err := eopa.Process.Signal(os.Interrupt); err != nil {
			panic(err)
		}
		eopa.Wait()
		if testing.Verbose() && t.Failed() {
			t.Logf("eopa stdout:\n%s", stdout.String())
			t.Logf("eopa stderr:\n%s", stderr.String())
		}
	})

	return eopa, &stdout, &stderr
}
