package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
)

var (
	apiKey   = flag.String("k", "asd", "api key")
	writekey = flag.Bool("w", false, "request write key")
	serial   = flag.String("s", "asd", "serial number")
)

type keyRequest struct {
	ApiKey       string `json:"apikey"`
	SerialNumber string `json:"serialnumber"`
	Write        bool   `json:"write"`
}

func main() {
	flag.Parse()
	ans := keyRequest{
		ApiKey:       *apiKey,
		SerialNumber: *serial,
		Write:        *writekey,
	}
	js, err := json.Marshal(ans)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(js))
	r, err := http.DefaultClient.Post("http://localhost:8090/api/request/key", "application/json", bytes.NewReader(js))
	if err != nil {
		panic(err)
	}
	body, _ := io.ReadAll(r.Body)
	fmt.Println(len(body))
	fmt.Println(string(body))
}
