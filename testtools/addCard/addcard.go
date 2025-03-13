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
	key    = flag.String("k", "asd", "api key")
	serial = flag.String("s", "asd", "serial number")
)

type addCardRequest struct {
	ApiKey       string `json:"apikey"`
	SerialNumber string `json:"serialnumber"`
}

func main() {
	flag.Parse()
	ans := addCardRequest{
		ApiKey:       *key,
		SerialNumber: *serial,
	}
	js, _ := json.Marshal(ans)
	fmt.Println(string(js))
	r, _ := http.DefaultClient.Post("http://localhost:8090/api/request/addCard", "application/json", bytes.NewReader(js))
	body, _ := io.ReadAll(r.Body)
	fmt.Println(len(body))
	fmt.Println(string(body))
}
