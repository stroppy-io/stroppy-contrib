package workload

import (
	"encoding/xml"
	"fmt"
	"net"
	"os"
	"strings"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/stroppy-io/stroppy/v6/pkg/bench"
)

type poolRoute struct {
	Connections string `xml:"connections"`
	Policy      string `xml:"policy"`
}

type poolConfig struct {
	names  []string
	routes [5]poolRoute
}

// Credentials remain in driver configuration and are never added to report data.
func loadPool(path string, drivers *bench.DriverDeclarations, clients int32, mysql bool) (*poolConfig, map[string]bench.DriverRef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var document struct {
		XMLName     xml.Name `xml:"connpool"`
		Connections struct {
			Entries []struct {
				XMLName       xml.Name
				Host          string `xml:"pg_host"`
				Port          string `xml:"pg_port"`
				SSLMode       string `xml:"pg_sslmode"`
				User          string `xml:"pg_user"`
				Password      string `xml:"pg_pass"`
				Database      string `xml:"pg_dbase"`
				MySQLHost     string `xml:"mysql_host"`
				MySQLPort     string `xml:"mysql_port"`
				MySQLSocket   string `xml:"mysql_socket"`
				MySQLSSL      string `xml:"mysql_ssl_options"`
				MySQLUser     string `xml:"mysql_user"`
				MySQLPassword string `xml:"mysql_pass"`
				MySQLDatabase string `xml:"mysql_dbase"`
			} `xml:",any"`
		} `xml:"connections"`
		Routes struct {
			NewOrder    poolRoute `xml:"neworder"`
			Payment     poolRoute `xml:"payment"`
			Delivery    poolRoute `xml:"delivery"`
			StockLevel  poolRoute `xml:"stocklevel"`
			OrderStatus poolRoute `xml:"orderstatus"`
		} `xml:"sprocs"`
	}
	if err := xml.Unmarshal(data, &document); err != nil {
		return nil, nil, err
	}
	pool := &poolConfig{routes: [5]poolRoute{document.Routes.NewOrder, document.Routes.Payment, document.Routes.Delivery, document.Routes.StockLevel, document.Routes.OrderStatus}}
	refs := make(map[string]bench.DriverRef)
	for _, connection := range document.Connections.Entries {
		name := connection.XMLName.Local
		if name == "default" || name == "" {
			return nil, nil, fmt.Errorf("invalid pool connection name %q", name)
		}
		if _, exists := refs[name]; exists {
			return nil, nil, fmt.Errorf("duplicate pool connection %q", name)
		}
		if mysql {
			cfg := gomysql.NewConfig()
			cfg.User, cfg.Passwd, cfg.DBName = connection.MySQLUser, connection.MySQLPassword, connection.MySQLDatabase
			cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(connection.MySQLHost, connection.MySQLPort)
			if (connection.MySQLHost == "localhost" || connection.MySQLHost == "127.0.0.1") && connection.MySQLSocket != "" && !strings.EqualFold(connection.MySQLSocket, "null") {
				cfg.Net, cfg.Addr = "unix", connection.MySQLSocket
			}
			if ssl := strings.TrimSpace(connection.MySQLSSL); ssl != "" && ssl != "-ssl false" {
				return nil, nil, fmt.Errorf("pool %s: configure MySQL TLS with -D%s driver options instead of XML ssl_options", name, name)
			}
			refs[name] = drivers.Declare(name, bench.DriverConfig{Kind: bench.DriverMySQL, URL: cfg.FormatDSN(), SQL: &bench.SQLConfig{MaxOpenConns: &clients}})
			pool.names = append(pool.names, name)
			continue
		}
		var values []string
		for _, pair := range [][2]string{{"host", connection.Host}, {"port", connection.Port}, {"sslmode", connection.SSLMode}, {"user", connection.User}, {"password", connection.Password}, {"dbname", connection.Database}} {
			value := strings.ReplaceAll(strings.ReplaceAll(pair[1], `\`, `\\`), "'", `\'`)
			values = append(values, pair[0]+"='"+value+"'")
		}
		pool.names = append(pool.names, name)
		mode := "cache_statement"
		refs[name] = drivers.Declare(name, bench.DriverConfig{Kind: bench.DriverPostgres, URL: strings.Join(values, " "), Postgres: &bench.PostgresConfig{DefaultQueryExecMode: &mode, MaxConns: &clients}})
	}
	for kind, route := range pool.routes {
		connections := strings.Fields(route.Connections)
		if len(connections) == 0 {
			return nil, nil, fmt.Errorf("pool route %s has no connections", transactionNames[kind])
		}
		seen := map[string]bool{}
		for _, name := range connections {
			if _, exists := refs[name]; !exists {
				return nil, nil, fmt.Errorf("pool route %s references unknown connection %q", transactionNames[kind], name)
			}
			if seen[name] {
				return nil, nil, fmt.Errorf("pool route %s repeats connection %q", transactionNames[kind], name)
			}
			seen[name] = true
		}
	}
	return pool, refs, nil
}

func (p *poolConfig) pick(kind int, count uint64, r *random) string {
	route := p.routes[kind]
	names := strings.Fields(route.Connections)
	switch route.Policy {
	case "last_named":
		return names[len(names)-1]
	case "random":
		return names[r.number(1, len(names))-1]
	case "round_robin":
		return names[count%uint64(len(names))]
	default:
		return names[0]
	}
}
