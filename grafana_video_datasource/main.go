package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"
)

func parseUnixTimeOrDefault(unixTsStr string, defaultTime time.Time) (time.Time, error) {
	if len(unixTsStr) == 0 {
		return defaultTime.Local(), nil
	}
	unixTs, err := strconv.ParseInt(unixTsStr, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(int64(unixTs), 0).Local(), nil
}

// toTs: exclusive
func listTargetDirectories(fromTs time.Time, toTs time.Time) []string {
	result := []string{}
	if fromTs.Year() != toTs.Add(-time.Nanosecond).Year() {
		nextFromTs := time.Date(fromTs.Year()+1, time.January, 1, 0, 0, 0, 0, fromTs.Location())
		result = append(result, listTargetDirectories(fromTs, nextFromTs)...)
		fromTs = nextFromTs
		for year := fromTs.Year(); year < toTs.Year(); year++ {
			result = append(result, filepath.Join(strconv.Itoa(year)))
			fromTs = fromTs.AddDate(1, 0, 0)
		}
	}
	if fromTs.Month() != toTs.Add(-time.Nanosecond).Month() {
		if fromTs.Month() == time.December {
			log.Panic("fromTs.Month() must not be Descember")
		}
		if fromTs.Month() > toTs.Month() {
			log.Panic("fromTs.Month() must not be larger than toTs")
		}
		nextFromTs := time.Date(fromTs.Year(), fromTs.Month()+1, 1, 0, 0, 0, 0, fromTs.Location())
		result = append(result, listTargetDirectories(fromTs, nextFromTs)...)
		fromTs = nextFromTs
		for month := fromTs.Month(); month < toTs.Month(); month++ {
			result = append(result, filepath.Join(fmt.Sprintf("%04d", fromTs.Year()), fmt.Sprintf("%02d", int(month))))
			fromTs = fromTs.AddDate(0, 1, 0)
		}
	}
	if fromTs.Day() != toTs.Add(-time.Nanosecond).Day() {
		nextFromTs := time.Date(fromTs.Year(), fromTs.Month(), fromTs.Day()+1, 0, 0, 0, 0, fromTs.Location())
		result = append(result, listTargetDirectories(fromTs, nextFromTs)...)
		fromTs = nextFromTs
		for day := fromTs.Day(); day < toTs.Day(); day++ {
			result = append(result, filepath.Join(fmt.Sprintf("%04d", fromTs.Year()), fmt.Sprintf("%02d", int(fromTs.Month())), fmt.Sprintf("%02d", day)))
			fromTs = fromTs.AddDate(0, 0, 1)
		}
	}
	if fromTs.Hour() != toTs.Add(-time.Nanosecond).Hour() {
		for hour := fromTs.Hour(); hour < toTs.Hour(); hour++ {
			result = append(result, filepath.Join(fmt.Sprintf("%04d", fromTs.Year()), fmt.Sprintf("%02d", int(fromTs.Month())), fmt.Sprintf("%02d", fromTs.Day()), fmt.Sprintf("%02d", hour)))
			fromTs = fromTs.Add(time.Hour)
		}
	}
	return result
}

func main() {
	var (
		port      = flag.String("port", "8080", "server port to listen")
		directory = flag.String("directory", "", "directory which contains image")
	)
	flag.Parse()
	http.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		fromTs, err := parseUnixTimeOrDefault(r.URL.Query().Get("from"), time.Now().Add(-24*time.Hour))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		toTs, err := parseUnixTimeOrDefault(r.URL.Query().Get("to"), fromTs.Add(24*time.Hour))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !fromTs.Before(toTs) {
			http.Error(w, "from should be less than to", http.StatusBadRequest)
			return
		}

		result := []string{}
		for _, d := range listTargetDirectories(fromTs, toTs) {
			filepath.WalkDir(filepath.Join(*directory, d), func(path string, d fs.DirEntry, err error) error {
				if d == nil {
					return nil
				}
				if d.Type().IsRegular() {
					rel, err := filepath.Rel(*directory, path)
					if err == nil {
						result = append(result, rel)
					}
				}
				return nil
			})
		}
		resultJson, err := json.Marshal(result)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(resultJson))
	})
	http.Handle("/file/", http.StripPrefix("/file/", http.FileServer(http.Dir(*directory))))
	http.ListenAndServe("0.0.0.0:"+*port, nil)
}
