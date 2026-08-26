package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const (
	NodeCount      = 108
	TestDuration   = 1 * time.Minute
	ChannelBufSize = 50000
	DBFile         = "iotDB_perf_test.db"
)

type SensorData struct {
	NodeID int
	TS     int64

	Value1 float32
	Value2 float32
}

var (
	generated uint64
	inserted  uint64
)

func main() {

	db := initDB()
	defer db.Close()

	dataChan := make(chan SensorData, ChannelBufSize)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		TestDuration,
	)
	defer cancel()

	var wg sync.WaitGroup

	// writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		sqliteWriter(ctx, db, dataChan)
	}()

	// generator
	wg.Add(1)
	go func() {
		defer wg.Done()
		centralSampler(ctx, dataChan)
	}()

	// monitor
	wg.Add(1)
	go func() {
		defer wg.Done()
		monitor(ctx)
	}()

	wg.Wait()

	validateData(db)

	printSummary(db)
}

func initDB() *sql.DB {

	db, err := sql.Open(
		"sqlite",
		DBFile,
	)

	if err != nil {
		log.Fatal(err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA cache_size=-65536;",
	}

	for _, p := range pragmas {

		_, err := db.Exec(p)

		if err != nil {
			log.Fatal(err)
		}
	}

	createTable := `
	CREATE TABLE IF NOT EXISTS sensor_data(
		ts INTEGER NOT NULL,
		node_id INTEGER NOT NULL,

		value1 REAL NOT NULL,
		value2 REAL NOT NULL,

		PRIMARY KEY(ts, node_id)
	);
	`

	_, err = db.Exec(createTable)

	if err != nil {
		log.Fatal(err)
	}

	return db
}

func centralSampler(
	ctx context.Context,
	ch chan<- SensorData,
) {

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {

		select {

		case <-ctx.Done():
			return

		case <-ticker.C:

			// 所有 Node 共用同一個 timestamp
			ts := time.Now().Unix()

			for nodeID := 1; nodeID <= NodeCount; nodeID++ {

				row := SensorData{
					NodeID: nodeID,
					TS:     ts,

					Value1: rand.Float32() * 100,
					Value2: rand.Float32() * 100,
				}

				ch <- row

				atomic.AddUint64(
					&generated,
					1,
				)
			}
		}
	}
}

func sqliteWriter(
	ctx context.Context,
	db *sql.DB,
	ch <-chan SensorData,
) {

	batch := make([]SensorData, 0, NodeCount)

	flush := func(rows []SensorData) {

		if len(rows) == 0 {
			return
		}

		tx, err := db.Begin()

		if err != nil {
			log.Println(err)
			return
		}

		stmt, err := tx.Prepare(`
			INSERT OR REPLACE
			INTO sensor_data
			(ts,node_id,value1,value2)
			VALUES (?,?,?,?)
		`)

		if err != nil {
			_ = tx.Rollback()
			log.Println(err)
			return
		}

		success := 0

		for _, row := range rows {

			_, err := stmt.Exec(
				row.TS,
				row.NodeID,
				row.Value1,
				row.Value2,
			)

			if err != nil {
				log.Println(err)
				continue
			}

			success++
		}

		stmt.Close()

		if err := tx.Commit(); err != nil {
			log.Println(err)
			return
		}

		atomic.AddUint64(
			&inserted,
			uint64(success),
		)
	}

	for {

		select {

		case <-ctx.Done():

			flush(batch)
			return

		case item := <-ch:

			batch = append(batch, item)

			// 收滿108筆立即寫入
			if len(batch) >= NodeCount {

				flush(batch)

				batch = batch[:0]
			}
		}
	}
}

func monitor(ctx context.Context) {

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var previous uint64

	for {

		select {

		case <-ctx.Done():
			return

		case <-ticker.C:

			current := atomic.LoadUint64(
				&inserted,
			)

			rate := (current - previous) / 10

			previous = current

			fmt.Printf(
				"[%s] inserts/sec=%d total=%d\n",
				time.Now().Format("15:04:05"),
				rate,
				current,
			)
		}
	}
}

func validateData(db *sql.DB) {

	fmt.Println()
	fmt.Println("=== Timestamp Validation ===")

	rows, err := db.Query(`
		SELECT ts, COUNT(*)
		FROM sensor_data
		GROUP BY ts
		ORDER BY ts DESC
		LIMIT 10
	`)

	if err != nil {
		log.Println(err)
		return
	}

	defer rows.Close()

	for rows.Next() {

		var ts int64
		var count int

		err := rows.Scan(
			&ts,
			&count,
		)

		if err != nil {
			continue
		}

		fmt.Printf(
			"%s -> %d rows\n",
			time.Unix(ts, 0).Format(
				"2006-01-02 15:04:05",
			),
			count,
		)
	}

	if err := rows.Err(); err != nil {
		log.Println(err)
	}
}

func printSummary(db *sql.DB) {

	var rowCount int64

	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM sensor_data
	`).Scan(&rowCount)

	if err != nil {
		log.Println(err)
	}

	fmt.Println()
	fmt.Println("=================================")
	fmt.Println("FINAL SUMMARY")
	fmt.Println("=================================")

	fmt.Printf(
		"Generated : %d\n",
		atomic.LoadUint64(&generated),
	)

	fmt.Printf(
		"Inserted  : %d\n",
		atomic.LoadUint64(&inserted),
	)

	fmt.Printf(
		"Rows In DB: %d\n",
		rowCount,
	)

	fmt.Printf(
		"Lost      : %d\n",
		atomic.LoadUint64(&generated)-
			atomic.LoadUint64(&inserted),
	)

	fmt.Println("=================================")
}
