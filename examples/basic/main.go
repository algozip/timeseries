package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/denisskin/timeseries"
)

func main() {
	const start int64 = 1700000100
	prices := timeseries.New(0, timeseries.Period1m, -2)
	prices.Push(start, 100.25)
	prices.Append(101.50)
	prices.Push(start+3*60, 102.75)

	cursor := prices.Cursor()
	for timestamp, price := range cursor.All() {
		fmt.Printf("%d price=%.2f sma=%.4f\n", timestamp, price, cursor.Avg(3*timeseries.Period1m))
	}
	from := time.Unix(start+60, 0)
	to := from.Add(2 * time.Minute)
	for timestamp, price := range cursor.Range(from.Unix(), to.Unix()) {
		fmt.Printf("Range: %d price=%.2f sma=%.4f\n", timestamp, price, cursor.Avg(3*timeseries.Period1m))
	}
	for timestamp, price := range prices.Tail(2 * timeseries.Period1m) {
		fmt.Printf("Tail: %d price=%.2f\n", timestamp, price)
	}
	fmt.Println("Cursor after stateless Tail:", cursor.Index())
	other := prices.Cursor().SeekTime(prices.LastTime())
	fmt.Println("Independent cursors:", cursor.Index(), other.Index())

	copy := prices.Slice(start+60, start+3*60)
	copy.Push(start+60, 99.25)
	fmt.Println("Copied range:", copy.Nums())
	fmt.Println("Original:", prices.Nums())

	reduced := prices.Reduce(2 * timeseries.Period1m)
	reduced.Push(reduced.NextTime(), 103.50)
	fmt.Println("Reduced:", reduced.Nums(), "cursor:", reduced.Cursor().Index())

	incoming := timeseries.New(0, timeseries.Period1m, -2)
	incoming.Push(start+3*60, 103.25)
	incoming.Push(start+5*60, 104.50)
	prices.Merge(incoming)
	cursor.SeekTime(start + 3*60)
	fmt.Println("Merged:", prices.Nums(), "sma:", cursor.Avg(3*timeseries.Period1m))

	fraction := 0.1
	fraction += 0.2
	fmt.Println("Original precision:", timeseries.PrecisionExp(fraction))
	computed := timeseries.New(0, timeseries.Period1s, -1)
	computed.Push(start, fraction)
	fmt.Println("Computed value:", computed.Nums())
	rounded := timeseries.New(start, timeseries.Period1m, -3)
	rounded.Append(1.005)
	rounded.SetExp(-2)
	fmt.Println("Rounded:", rounded.Nums())

	dir, err := os.MkdirTemp("", "timeseries-example-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	filename := filepath.Join(dir, "prices.zts")
	if err := prices.WriteToFile(filename); err != nil {
		log.Fatal(err)
	}
	restored, err := timeseries.TimeSeriesRead(filename)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Restored:", restored.Nums())
}
