package tests_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dosco/graphjin/cassandradriver"
	"github.com/gocql/gocql"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const cassandraKeyspace = "graphjin_test"

// startCassandraDB boots a cassandra:5 container, seeds the shared webshop schema
// (same data the other dialects use), and returns a *sql.DB backed by the
// cassandradriver connector. Cassandra runs the shared Example/Test suite like
// MongoDB; per-test cassandra branches handle shapes CQL can't serve.
func startCassandraDB(ctx context.Context) (func(context.Context) error, *sql.DB, error) {
	// Reuse an already-running cluster (fast local iteration) when pointed at one.
	if hosts := os.Getenv("CASSANDRA_TEST_HOSTS"); hosts != "" {
		cluster := gocql.NewCluster(strings.Split(hosts, ",")...)
		cluster.ProtoVersion = 4
		cluster.Consistency = gocql.Quorum
		cluster.ConnectTimeout = 20 * time.Second
		cluster.Timeout = 20 * time.Second
		session, err := cluster.CreateSession()
		if err != nil {
			return nil, nil, fmt.Errorf("cassandra session (reuse): %w", err)
		}
		if err := seedCassandraWebshop(session); err != nil {
			session.Close()
			return nil, nil, fmt.Errorf("cassandra seed: %w", err)
		}
		db := sql.OpenDB(cassandradriver.NewConnector(session, cassandraKeyspace))
		return func(context.Context) error { db.Close(); session.Close(); return nil }, db, nil
	}

	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "cassandra:5",
			ExposedPorts: []string{"9042/tcp"},
			Env: map[string]string{
				"CASSANDRA_DC":              "dc1",
				"CASSANDRA_ENDPOINT_SNITCH": "GossipingPropertyFileSnitch",
				"MAX_HEAP_SIZE":             "512M",
				"HEAP_NEWSIZE":              "128M",
			},
			WaitingFor: wait.ForListeningPort("9042/tcp").WithStartupTimeout(300 * time.Second),
		},
		Started: true,
	}
	container, err := testcontainers.GenericContainer(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("cassandra container: %w", err)
	}
	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, err
	}
	port, err := container.MappedPort(ctx, "9042")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, err
	}

	cluster := gocql.NewCluster(host)
	cluster.Port = port.Int()
	cluster.ProtoVersion = 4
	cluster.Consistency = gocql.Quorum
	cluster.ConnectTimeout = 20 * time.Second
	cluster.Timeout = 20 * time.Second

	var session *gocql.Session
	for i := 0; i < 90; i++ {
		if session, err = cluster.CreateSession(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("cassandra session: %w", err)
	}

	if err := seedCassandraWebshop(session); err != nil {
		session.Close()
		_ = container.Terminate(ctx)
		return nil, nil, fmt.Errorf("cassandra seed: %w", err)
	}

	sqlDB := sql.OpenDB(cassandradriver.NewConnector(session, cassandraKeyspace))
	cleanup := func(ctx context.Context) error {
		_ = sqlDB.Close()
		session.Close()
		return container.Terminate(ctx)
	}
	return cleanup, sqlDB, nil
}

// createdAt mirrors the timestamp the other dialects seed.
var cassandraCreatedAt = time.Date(2021, 1, 9, 16, 37, 1, 0, time.UTC)

func seedCassandraWebshop(s *gocql.Session) error {
	ddl := []string{
		`CREATE KEYSPACE IF NOT EXISTS graphjin_test WITH replication = {'class':'SimpleStrategy','replication_factor':1}`,
		`CREATE TABLE IF NOT EXISTS graphjin_test.users (id int PRIMARY KEY, full_name text, email text, phone text, stripe_id text, disabled boolean, created_at timestamp)`,
		`CREATE TABLE IF NOT EXISTS graphjin_test.products (id int PRIMARY KEY, name text, description text, price double, owner_id int, country_code text, created_at timestamp)`,
		`CREATE TABLE IF NOT EXISTS graphjin_test.purchases (id int PRIMARY KEY, customer_id int, product_id int, quantity int, created_at timestamp)`,
		`CREATE TABLE IF NOT EXISTS graphjin_test.comments (id int PRIMARY KEY, body text, product_id int, commenter_id int, reply_to_id int, created_at timestamp)`,
		`CREATE TABLE IF NOT EXISTS graphjin_test.categories (id int PRIMARY KEY, name text, description text, created_at timestamp)`,
		`CREATE TABLE IF NOT EXISTS graphjin_test.chats (id int PRIMARY KEY, body text, created_at timestamp)`,
		`CREATE TABLE IF NOT EXISTS graphjin_test.notifications (id int PRIMARY KEY, verb text, subject_type text, subject_id int, user_id int, created_at timestamp)`,
	}
	for _, q := range ddl {
		if err := s.Query(q).Exec(); err != nil {
			return err
		}
	}

	for i := 1; i <= 100; i++ {
		if err := s.Query(`INSERT INTO graphjin_test.users (id, full_name, email, phone, stripe_id, disabled, created_at) VALUES (?,?,?,?,?,?,?)`,
			i, fmt.Sprintf("User %d", i), fmt.Sprintf("user%d@test.com", i), nil,
			fmt.Sprintf("payment_id_%d", i+1000), i == 50, cassandraCreatedAt).Exec(); err != nil {
			return err
		}
		if err := s.Query(`INSERT INTO graphjin_test.products (id, name, description, price, owner_id, country_code, created_at) VALUES (?,?,?,?,?,?,?)`,
			i, fmt.Sprintf("Product %d", i), fmt.Sprintf("Description for product %d", i),
			float64(i)+10.5, i, "US", cassandraCreatedAt).Exec(); err != nil {
			return err
		}
		customerID := i + 1
		if i >= 100 {
			customerID = 1
		}
		if err := s.Query(`INSERT INTO graphjin_test.purchases (id, customer_id, product_id, quantity, created_at) VALUES (?,?,?,?,?)`,
			i, customerID, i, i*10, cassandraCreatedAt).Exec(); err != nil {
			return err
		}
		replyTo := 0
		if i >= 2 {
			replyTo = i - 1
		}
		if err := s.Query(`INSERT INTO graphjin_test.comments (id, body, product_id, commenter_id, reply_to_id, created_at) VALUES (?,?,?,?,?,?)`,
			i, fmt.Sprintf("This is comment number %d", i), i, i, replyTo, cassandraCreatedAt).Exec(); err != nil {
			return err
		}
	}
	for i := 1; i <= 5; i++ {
		if err := s.Query(`INSERT INTO graphjin_test.categories (id, name, description, created_at) VALUES (?,?,?,?)`,
			i, fmt.Sprintf("Category %d", i), fmt.Sprintf("Description for category %d", i), cassandraCreatedAt).Exec(); err != nil {
			return err
		}
		if err := s.Query(`INSERT INTO graphjin_test.chats (id, body, created_at) VALUES (?,?,?)`,
			i, fmt.Sprintf("This is chat message number %d", i), cassandraCreatedAt).Exec(); err != nil {
			return err
		}
	}
	return nil
}
