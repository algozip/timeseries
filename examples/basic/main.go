package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/denisskin/timeseries"
)

func main() {
	const start int64 = 1700000040
	prices := timeseries.New(start, timeseries.Period1m, -2)
	prices.Append(100.25)
	prices.Append(101.50)
	prices.Push(timeseries.Tick{Time: start + 3*60, Ask: 102.75})

	for timestamp, price := range prices.Iterator {
		fmt.Printf("%d price=%.2f sma=%.4f\n", timestamp, price, prices.Avg(3*60))
	}

	dir, err := os.MkdirTemp("", "timeseries-example-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	filename := filepath.Join(dir, "prices.zi")
	if err := prices.WriteToFile(filename); err != nil {
		log.Fatal(err)
	}
	restored, err := timeseries.TimeSeriesRead(filename)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Restored:", restored.Nums())
	fmt.Println("Moving average:", restored.MovingAverage(3*timeseries.Period1m).Nums())
	fmt.Println("Changes:", restored.Diff(1))
}
