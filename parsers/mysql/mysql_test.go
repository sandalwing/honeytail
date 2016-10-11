package mysql

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/honeycombio/honeytail/event"
	"github.com/honeycombio/mysqltools/query/normalizer"
)

type slowQueryData struct {
	rawE      []string
	sq        map[string]interface{}
	timestamp time.Time
}

const (
	hostLine  = "# User@Host: root[root] @ localhost [127.0.0.1]  Id:   136"
	timerLine = "# Query_time: 0.000171  Lock_time: 0.000000 Rows_sent: 1  Rows_examined: 0"
	useLine   = "use honeycomb;"
)

var t1, _ = time.Parse("02/Jan/2006:15:04:05.000000", "01/Apr/2016:00:31:09.817887")
var t2, _ = time.Parse("02/Jan/2006:15:04:05.000000", "01/Apr/2016:00:31:10.817887")
var tUnparseable, _ = time.Parse("02/Jan/2006:15:04:05.000000", "02/Aug/2010:13:24:56")

// `Time` field has ms resolution but no time zone; `SET timestamp=` is UNIX timestamp but no ms resolution
// e: mysql… i guess we could/should just combine the unix timestamp second and the parsed timestamp ms
// justify parsing both
// could be terrible

var sqds = []slowQueryData{
	{
		rawE: []string{
			"# Time: 2016-04-01T00:31:09.817887Z",
			"# Query_time: 0.008393  Lock_time: 0.000154 Rows_sent: 1  Rows_examined: 357",
		},
		sq: map[string]interface{}{
			queryTimeKey:    0.008393,
			lockTimeKey:     0.000154,
			rowsSentKey:     1,
			rowsExaminedKey: 357,
		},
		timestamp: t1,
	},
	{
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"# User@Host: someuser @ hostfoo [192.168.2.1]  Id:   666",
		},
		sq: map[string]interface{}{
			userKey:     "someuser",
			clientKey:   "hostfoo",
			clientIPKey: "192.168.2.1",
		},
		timestamp: tUnparseable,
	},
	{
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"# User@Host: root @ localhost []  Id:   233",
		},
		sq: map[string]interface{}{
			userKey:     "root",
			clientKey:   "localhost",
			clientIPKey: "",
		},
		timestamp: tUnparseable,
	},
	{
		// RDS style user host line
		rawE: []string{
			"# User@Host: root[root] @  [10.0.1.76]  Id: 325920",
		},
		sq: map[string]interface{}{
			userKey:     "root",
			clientKey:   "",
			clientIPKey: "10.0.1.76",
		},
		timestamp: tUnparseable,
	},
	{
		// RDS style user host line with hostname
		rawE: []string{
			"# User@Host: root[root] @ foobar [10.0.1.76]  Id: 325920",
		},
		sq: map[string]interface{}{
			userKey:     "root",
			clientKey:   "foobar",
			clientIPKey: "10.0.1.76",
		},
		timestamp: tUnparseable,
	},
	{
		rawE: []string{
			"# Time: not-a-recognizable time stamp",
			"# administrator command: Ping;",
		},
		sq:        nil,
		timestamp: tUnparseable,
	},
	{
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"SET timestamp=1459470669;",
			"show status like 'Uptime';",
		},
		sq: map[string]interface{}{
			queryKey:           "show status like 'Uptime'",
			normalizedQueryKey: "show status like ?",
			statementKey:       "",
		},
		timestamp: t1.Truncate(time.Second),
	},
	{
		// fails to parse "Time" but we capture unix time and we fall back to the scan normalizer
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"SET timestamp=1459470669;",
			"SELECT * FROM (SELECT  T1.orderNumber,  STATUS,  SUM(quantityOrdered * priceEach) AS  total FROM orders WHERE total > 1000 AS T1 INNER JOIN orderdetails AS T2 ON T1.orderNumber = T2.orderNumber GROUP BY  orderNumber) T WHERE total > 100;",
		},
		sq: map[string]interface{}{
			queryKey:           "SELECT * FROM (SELECT  T1.orderNumber,  STATUS,  SUM(quantityOrdered * priceEach) AS  total FROM orders WHERE total > 1000 AS T1 INNER JOIN orderdetails AS T2 ON T1.orderNumber = T2.orderNumber GROUP BY  orderNumber) T WHERE total > 100",
			normalizedQueryKey: "select * from (select t1.ordernumber, status, sum(quantityordered * priceeach) as total from orders where total > ? as t1 inner join orderdetails as t2 on t1.ordernumber = t2.ordernumber group by ordernumber) t where total > ?",
			statementKey:       "",
		},
		timestamp: t1.Truncate(time.Second),
	},
	{
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"SET timestamp=1459470669;",
			"SELECT * FROM orders WHERE total > 1000;",
		},
		sq: map[string]interface{}{
			queryKey:           "SELECT * FROM orders WHERE total > 1000",
			normalizedQueryKey: "select * from orders where total > ?",
			tablesKey:          "orders",
			statementKey:       "select",
		},
		timestamp: t1.Truncate(time.Second),
	},
	{
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"SET timestamp=1459470669;",
			"SELECT *",
			"FROM orders WHERE",
			"total > 1000;",
		},
		sq: map[string]interface{}{
			queryKey:           "SELECT * FROM orders WHERE total > 1000",
			normalizedQueryKey: "select * from orders where total > ?",
			tablesKey:          "orders",
			statementKey:       "select",
		},
		timestamp: t1.Truncate(time.Second),
	},
	{
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"SET timestamp=1459470669;",
			"use someDB;",
		},
		sq: map[string]interface{}{
			databaseKey:        "someDB",
			queryKey:           "use someDB",
			normalizedQueryKey: "use someDB",
		},
		timestamp: t1.Truncate(time.Second),
	},
	{
		// a use as well as query
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"SET timestamp=1459470669;",
			"use someDB;",
			"SELECT *",
			"FROM orders WHERE",
			"total > 1000;",
		},
		sq: map[string]interface{}{
			databaseKey:        "someDB",
			queryKey:           "SELECT * FROM orders WHERE total > 1000",
			normalizedQueryKey: "select * from orders where total > ?",
			tablesKey:          "orders",
			statementKey:       "select",
		},
		timestamp: t1.Truncate(time.Second),
	},
	// some tests for corrupted logs
	{
		// invalid query + use + query, ignore the invalid query
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"SET timestamp=1459470669;",
			"SELECT *",
			"use someDB;",
			"SELECT *",
			"FROM orders WHERE",
			"total > 1000;",
		},
		sq: map[string]interface{}{
			databaseKey:        "someDB",
			queryKey:           "SELECT * FROM orders WHERE total > 1000",
			normalizedQueryKey: "select * from orders where total > ?",
			tablesKey:          "orders",
			statementKey:       "select",
		},
		timestamp: t1.Truncate(time.Second),
	},
	{
		// invalid query + set time + query, ignore the invalid query
		rawE: []string{
			"# Time: not-a-parsable-time-stampZ",
			"SET timestamp=1459470669;",
			"SELECT * FROM orders WHERE",
			"SET timestamp=1459470670;",
			"SELECT * FROM orders WHERE total > 1000;",
		},
		sq: map[string]interface{}{
			queryKey:           "SELECT * FROM orders WHERE total > 1000",
			normalizedQueryKey: "select * from orders where total > ?",
			tablesKey:          "orders",
			statementKey:       "select",
		},
		timestamp: t2.Truncate(time.Second),
	},
	{
		// query + query_time comment + query, ignore the first query
		rawE: []string{
			"# Time: 2016-04-01T00:31:09.817887Z",
			"SELECT * FROM orders WHERE total < 1000;",
			"# Query_time: 0.008393  Lock_time: 0.000154 Rows_sent: 1  Rows_examined: 357",
			"SELECT * FROM orders WHERE total > 1000;",
		},
		sq: map[string]interface{}{
			queryTimeKey:       0.008393,
			lockTimeKey:        0.000154,
			rowsSentKey:        1,
			rowsExaminedKey:    357,
			queryKey:           "SELECT * FROM orders WHERE total > 1000",
			normalizedQueryKey: "select * from orders where total > ?",
			tablesKey:          "orders",
			statementKey:       "select",
		},
		timestamp: t1,
	},
	{
		// invalid query + user@host comment + query, ignore the invalid query
		rawE: []string{
			"# Time: 2016-04-01T00:31:09.817887Z",
			"SELECT * FROM orders WHERE",
			"# User@Host: someuser @ hostfoo [192.168.2.1]  Id:   666",
			"SELECT * FROM orders WHERE total > 1000;",
		},
		sq: map[string]interface{}{
			userKey:            "someuser",
			clientKey:          "hostfoo",
			clientIPKey:        "192.168.2.1",
			queryKey:           "SELECT * FROM orders WHERE total > 1000",
			normalizedQueryKey: "select * from orders where total > ?",
			tablesKey:          "orders",
			statementKey:       "select",
		},
		timestamp: t1,
	},
	{
		// query without its last line
		rawE: []string{
			"# Time: 2016-04-01T00:31:09.817887Z",
			"SELECT * FROM orders",
		},
		sq:        map[string]interface{}{},
		timestamp: t1,
	},
	{
		rawE: []string{},
		sq:   map[string]interface{}{},
	},
}

func TestHandleEvent(t *testing.T) {
	p := &Parser{
		nower:      &FakeNower{},
		normalizer: &normalizer.Parser{},
	}
	for i, sqd := range sqds {
		res, timestamp := p.handleEvent(sqd.rawE)
		if !reflect.DeepEqual(res, sqd.sq) {
			t.Errorf("case num %d: expected\n %+v, got\n %+v", i, sqd.sq, res)
		}
		if timestamp.UnixNano() != sqd.timestamp.UnixNano() {
			t.Errorf("case num %d: time parsed incorrectly:\n\tExpected: %+v, Actual: %+v",
				i, sqd.timestamp, timestamp)
		}
	}
}

func TestTimeProcessing(t *testing.T) {
	p := &Parser{
		nower: &FakeNower{},
	}
	tsts := []struct {
		lines    []string
		expected time.Time
	}{
		{[]string{
			"# Time: 2016-09-15T10:16:17.898325Z", hostLine, timerLine, useLine,
			"SET timestamp=1473934577;",
		}, time.Date(2016, time.September, 15, 10, 16, 17, 898325000, time.UTC)},
		{[]string{
			"# Time: not-a-parsable-time-stampZ", hostLine, timerLine, useLine,
			"SET timestamp=1459470669;",
		}, time.Date(2016, time.April, 1, 0, 31, 9, 0, time.UTC)},
		{[]string{
			"# Time: 2016-09-16T19:37:39.006083Z", hostLine, timerLine, useLine,
		}, time.Date(2016, time.September, 16, 19, 37, 39, 6083000, time.UTC)},
		{[]string{hostLine, timerLine, useLine}, p.nower.Now()},
	}

	for _, tt := range tsts {
		_, timestamp := p.handleEvent(tt.lines)
		if timestamp.Unix() != tt.expected.Unix() {
			t.Errorf("Didn't capture unix ts from lines:\n%+v\n\tExpected: %d, Actual: %d",
				strings.Join(tt.lines, "\n"), tt.expected.Unix(), timestamp.Unix())
		}
		if timestamp.Nanosecond() != tt.expected.Nanosecond() {
			t.Errorf("Didn't capture time with MS resolution from lines:\n%+v\n\tExpected: %d, Actual: %d",
				strings.Join(tt.lines, "\n"), tt.expected.Nanosecond(), timestamp.Nanosecond())
		}
	}
}

// test that ProcessLines correctly splits the mysql slow query log stream into
// individual events. It should read in alternating sets of commented then
// uncommented lines and split them at the first comment after an uncommented
// line.
func TestProcessLines(t *testing.T) {
	p := &Parser{
		nower:      &FakeNower{},
		normalizer: &normalizer.Parser{},
	}
	lines := make(chan string, 10)
	send := make(chan event.Event, 5)
	go p.ProcessLines(lines, send)
	for _, line := range []string{
		"# administrator command: Prepare;",
		"# Time: 2016-04-01T00:31:09.817887Z",
		"# User@Host: someuser @ hostfoo [192.168.2.1]  Id:   666",
		"# Query_time: 0.000073  Lock_time: 0.000000 Rows_sent: 0  Rows_examined: 0",
		"SELECT * FROM orders WHERE total > 1000;",
		"# administrator command: Prepare;",
		"# Time: 2016-04-01T00:31:09.817887Z",
		"# User@Host: otheruser @ hostbar [192.168.2.1]  Id:   666",
		"# Query_time: 0.00457  Lock_time: 0.1 Rows_sent: 5  Rows_examined: 35",
		"SELECT * FROM",
		"customers;",
	} {
		lines <- line
	}
	close(lines)
	ev1 := <-send
	ev2 := <-send
	ts, _ := time.Parse(time.RFC3339Nano, "2016-04-01T00:31:09.817887Z")
	expectedEv1 := event.Event{
		Timestamp: ts,
		Data: map[string]interface{}{
			"client":           "hostfoo",
			"client_ip":        "192.168.2.1",
			"user":             "someuser",
			"query_time":       0.000073,
			"lock_time":        0.0,
			"rows_sent":        0,
			"rows_examined":    0,
			"query":            "SELECT * FROM orders WHERE total > 1000",
			"normalized_query": "select * from orders where total > ?",
			"tables":           "orders",
			"statement":        "select",
		},
	}
	expectedEv2 := event.Event{
		Timestamp: ts,
		Data: map[string]interface{}{
			"client":           "hostbar",
			"client_ip":        "192.168.2.1",
			"user":             "otheruser",
			"query_time":       0.00457,
			"lock_time":        0.1,
			"rows_sent":        5,
			"rows_examined":    35,
			"query":            "SELECT * FROM customers",
			"normalized_query": "select * from customers",
			"tables":           "customers",
			"statement":        "select",
		},
	}
	if !reflect.DeepEqual(ev1, expectedEv1) {
		t.Errorf("ev1 mismatch. got %+v, expected %+v", ev1, expectedEv1)
	}
	if !reflect.DeepEqual(ev2, expectedEv2) {
		t.Errorf("ev2 mismatch. got %+v, expected %+v", ev2, expectedEv2)
	}
}

type FakeNower struct{}

func (f *FakeNower) Now() time.Time {
	fakeTime, _ := time.Parse("02/Jan/2006:15:04:05.000000 -0700", "02/Aug/2010:13:24:56 -0000")
	return fakeTime
}
