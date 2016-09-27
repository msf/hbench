package hbench

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime/pprof"
	"sort"
	"time"
)

type AggregatedValues struct {
	Values []float64
	Counts map[int]int
	Total  float64
}

type PercentileValues struct {
	Percentiles map[int]float32
	Count       int
	Average     float32
	Min         float32
	Max         float32
}

var (
	PERCENTILES   = [...]int{10, 50, 90, 99, 100}
	cpuprofile    = flag.String("cpuprofile", "", "write cpu profile to file")
	totalRequests = flag.Int("reqs", 100, "total request count")
	urlsFile      = flag.String("urlfile", "", "file with 1 url per line")
	url           = flag.String("url", "", "url target")
	concurrency   = flag.Int("concurrency", 2, "number of concurrent requests")

//	doPost        = flag.Bool("POST", "", "do posts")
//	postBodyFile  = flag.String("postbodyfile", "", "file with 1 post body per line")
)

func main() {
	argOffset := 1
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
		argOffset++
	}

	log.Println("shit is happening")
	c := make(chan string, *concurrency*10)

	results := make(AggregatedValues, *concurrency)

	for i := 0; i < *concurrency; i++ {
		results[i] = AggregatedValues{
			Values: make([]float64, 0),
			Counts: make(map[int]int),
		}
		go doHttpReq(&results[i], c)
	}
	generateReqs(url, urlsFile, *totalRequests, c)
	close(c)

	values := processResults(results)
	printResults(values)

	percentiles := computePercentiles(values, PERCENTILES[:])
	printPercentiles(percentiles)
}

func generateReqs(url *string, urlFile *string, totalRequests int, channel chan string) {

	if url != nil {
		for i := 0; i < totalRequests; i++ {
			channel <- *url
		}
	} else {
		if urlFile == nil {
			log.Fatal("bad args, either 'url' or 'urlsFile' should be used")
		}
		lines := make([]string)
		// enter uglyness
		f, _ := os.Open(filename)
		defer f.Close()
		scanner := bufio.NewScanner(f)

		for scanner.Scan() {
			line := scanner.Text()
			lines = append(lines, line)
		}
		if err := scanner.Err(); err != nil {
			log.Printf("error reading file: %s, err:%v", filename, err)
		}
		count := len(lines)
		for i := 0; i < totalRequests; i++ {
			channel <- lines[i%count]
		}
	}

	close(channel)
}

type reqResp struct {
	StatusCode int
	BodySize   int
}

func timeit(accumulator *AggregatedValues, fn func() reqResp) {
	start := time.Now()
	res := fn()
	elapsed := time.Since(start)
	accumulator.Values = append(accumulator.Values, elapsed.Seconds())
	accumulator.Counts[res.statusCode]++
	accumulator.Total += elapsed.Seconds()
}

func doHttpReq(accum *AggregatedValues, channel chan string) {
	for url := range channel {
		timeit(accum, func() {
			var myResp reqResp
			resp, err := http.Get(url)
			if err != nil {
				log.Println("http get failed, error:", err)
				return repResp{010, 0}
			}
			myResp.StatusCode = resp.StatusCode
			defer resp.Body.Close()
			buf, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				return repResp{020, 0}
			}
			return repResp{resp.StatusCode, len(buf)}
		})
	}
}

func processResults(aggregates []AggregatedValues) AggregatedValues {

	totalReqs := 0
	for _, agg := range aggregates {
		totalReqs += len(agg.Values)
	}

	result := AggregatedValues{
		Values: make([]float64, totalReqs),
		Counts: make(map[int]int),
	}

	for _, agg := range aggregates {
		result.Values = append(result.Values, agg.Values...)
		result.Total += agg.Total
		for k, v := range agg.Counts {
			result[k] += v
		}
	}
}

func getPercentile(sortedValues []float64, percentile int) float64 {
	count := len(sortedValues)
	if count == 0 {
		return -1
	} else if count == 1 {
		return sortedValues[0]
	}
	if percentile >= 100 {
		return sortedValues[count-1]
	}

	pos := (percentile * count) / 100
	return sortedValues[pos]
}

func computePercentiles(values AggregatedValues, percentiles []int) PercentileValues {

	sort.Float64s(values.Values)
	count := len(values.Values)
	result := PercentileValues{
		Percentiles: make(map[int]float64, len(percentiles)),
	}
	if count == 0 {
		return result
	}

	result.Average = values.Accum / float64(count)
	result.Min = values.Values[0]
	result.Max = values.Values[count-1]
	result.Count = count

	for _, percent := range percentiles {
		result.Percentiles[percent] = getPercentile(values.Values, percent)
	}

	return result
}

func printPercentiles(values PercentileValues) {

	keys := make([]int, 0, len(values.Percentiles))
	for k := range values.Percentiles {
		keys = append(keys, k)
	}

	sort.Ints(keys)
	summary := fmt.Sprintf("count: %d,    min: %.3f,    avg: %.3f,    max: %.3f\n",
		values.Count, values.Min, values.Average, values.Max)
	for _, k := range keys {
		summary += fmt.Sprintf("P%d%%: %.3f,    ", k, values.Percentiles[k])
	}
	log.Print(summary)
}
