package cassandra

import (
	"fmt"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const connectTimeout = 5 * time.Second

func Open(hosts []string, keyspace string) (*gocql.Session, error) {
	cluster := gocql.NewCluster(hosts...)
	cluster.Keyspace = keyspace
	cluster.Consistency = gocql.LocalQuorum
	cluster.ConnectTimeout = connectTimeout

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("cassandra: open: %w", err)
	}
	return session, nil
}
