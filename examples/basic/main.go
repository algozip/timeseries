package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ztape/timeseries"
)

func main() {
	start := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	prices := timeseries.New(start, time.Minute, -2)
	prices.Push(start, 100.25)
	prices.Append(101.50)
	prices.Push(start.Add(3*time.Minute), 102.75)

	cursor := prices.Cursor()
	for timestamp, price := range cursor.All() {
		fmt.Printf("%s price=%.2f sma=%.4f\n", timestamp.Format(time.RFC3339Nano), price, cursor.Avg(3*time.Minute))
	}
	window := prices.Range(start.Add(time.Minute), start.Add(3*time.Minute))
	for timestamp, price := range window.AllUnix() {
		fmt.Printf("Unix range: %d price=%.2f sma=%.4f\n", timestamp, price, window.Avg(3*time.Minute))
	}
	fmt.Println("Range bounds:", window.StartIndex(), window.EndIndex())
	for timestamp, price := range prices.Tail(2 * time.Minute).All() {
		fmt.Printf("Tail: %s price=%.2f\n", timestamp.Format(time.RFC3339Nano), price)
	}
	fmt.Println("Metadata:", prices.StartTime(), prices.Step(), prices.Duration())

	copy := prices.Slice(start.Add(time.Minute), start.Add(3*time.Minute))
	copy.Push(copy.StartTime(), 99.25)
	fmt.Println("Copied range:", copy.Nums(), "original:", prices.Nums())

	reduced := prices.Reduce(2 * time.Minute)
	reduced.Push(reduced.NextTime(), 103.50)
	fmt.Println("Reduced:", reduced.Nums(), "cursor:", reduced.Cursor().Index())

	incoming := timeseries.New(start, time.Minute, -2)
	incoming.Push(start.Add(3*time.Minute), 103.25)
	incoming.Push(start.Add(5*time.Minute), 104.50)
	prices.Merge(incoming)
	cursor.Seek(start.Add(3 * time.Minute))
	fmt.Println("Merged:", prices.Nums(), "sma:", cursor.Avg(3*time.Minute))

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
